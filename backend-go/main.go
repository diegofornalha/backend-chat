package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/cors"
	"golang.org/x/time/rate"
)

type ChatRequest struct {
	Message     string  `json:"message"`
	SessionID   *string `json:"session_id"`
	ProjectName *string `json:"project_name"`
}

type SSEMessage struct {
	Type      string  `json:"type"`
	Content   string  `json:"content"`
	SessionID *string `json:"session_id,omitempty"`
}

// SessionInfo representa informações sobre uma sessão
type SessionInfo struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// cacheEntry representa uma entrada no cache com timestamp
type cacheEntry struct {
	sessions  []SessionInfo
	timestamp time.Time
}

// sessionCache é o cache global de sessões por projeto
var sessionCache = struct {
	sync.RWMutex
	data map[string]cacheEntry
}{
	data: make(map[string]cacheEntry),
}

// messageQueue representa uma fila de mensagens para uma sessão
type messageQueue struct {
	messages   []queuedMessage
	processing bool
	mu         sync.Mutex
}

// queuedMessage representa uma mensagem enfileirada com seu contexto
type queuedMessage struct {
	message  ChatRequest
	response chan<- sseEvent
	ctx      context.Context
}

// sseEvent representa um evento SSE a ser enviado ao cliente
type sseEvent struct {
	eventType string // "text", "error", "done"
	content   string
	sessionID *string
}

// sessionQueues mantém filas de mensagens por session_id
var sessionQueues = struct {
	sync.RWMutex
	queues map[string]*messageQueue
}{
	queues: make(map[string]*messageQueue),
}

// getOrCreateQueue obtém ou cria uma fila para uma sessão
func getOrCreateQueue(sessionID string) *messageQueue {
	sessionQueues.Lock()
	defer sessionQueues.Unlock()

	queue, exists := sessionQueues.queues[sessionID]
	if !exists {
		queue = &messageQueue{
			messages:   make([]queuedMessage, 0),
			processing: false,
		}
		sessionQueues.queues[sessionID] = queue
		log.Printf("📋 Fila criada para sessão: %s", sessionID)
	}
	return queue
}

// enqueueMessage adiciona uma mensagem à fila da sessão
func (q *messageQueue) enqueue(msg queuedMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, msg)
	log.Printf("➕ Mensagem enfileirada (total: %d)", len(q.messages))
}

// dequeue remove e retorna a primeira mensagem da fila
func (q *messageQueue) dequeue() (queuedMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.messages) == 0 {
		return queuedMessage{}, false
	}

	msg := q.messages[0]
	q.messages = q.messages[1:]
	log.Printf("➖ Mensagem desenfileirada (restantes: %d)", len(q.messages))
	return msg, true
}

// isProcessing verifica se a fila está processando
func (q *messageQueue) isProcessing() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.processing
}

// setProcessing define o estado de processamento da fila
func (q *messageQueue) setProcessing(processing bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.processing = processing
}

// processQueue processa mensagens da fila sequencialmente
func processQueue(sessionID string, projectDir string, initialSessionExists bool) {
	queue := getOrCreateQueue(sessionID)

	for {
		msg, hasMore := queue.dequeue()
		if !hasMore {
			queue.setProcessing(false)
			log.Printf("✅ Fila vazia, processamento finalizado para sessão: %s", sessionID)
			return
		}

		log.Printf("⚙️  Processando mensagem da fila para sessão: %s", sessionID)

		// Verificar se sessão existe ANTES de cada execução (pode ter sido criada por mensagem anterior)
		sessionFile := filepath.Join(projectDir, sessionID+".jsonl")
		sessionExists := false
		if _, err := os.Stat(sessionFile); err == nil {
			sessionExists = true
			log.Printf("✅ Sessão existe, usando --continue: %s", sessionFile)
		} else {
			log.Printf("📝 Sessão não existe, criando nova: %s", sessionFile)
		}

		executeClaudeCLI(msg.ctx, msg.message, sessionID, projectDir, sessionExists, msg.response)
	}
}

// executeClaudeCLI agora faz proxy para o backend Python que usa o SDK oficial
func executeClaudeCLI(ctx context.Context, req ChatRequest, sessionID string, projectDir string, sessionExists bool, eventChan chan<- sseEvent) {
	defer close(eventChan)

	log.Printf("🔄 Proxy para Python SDK - Sessão: %s", sessionID)

	// Extrair project_id do projectDir
	// projectDir = /Users/2a/.claude/projetos/teste-memoria
	// project_id deve ser apenas o nome final: teste-memoria
	projectID := filepath.Base(projectDir)

	log.Printf("📦 project_id extraído: %s (de %s)", projectID, projectDir)

	// Preparar payload para o Python backend
	payload := map[string]interface{}{
		"message":    req.Message,
		"session_id": sessionID,
		"project_id": projectID,
		"cwd":        projectDir, // Caminho completo do projeto para o SDK usar como working directory
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		eventChan <- sseEvent{eventType: "error", content: fmt.Sprintf("Erro ao criar payload: %v", err)}
		return
	}

	// Fazer requisição HTTP para o Python backend
	pythonURL := "http://localhost:8080/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", pythonURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		eventChan <- sseEvent{eventType: "error", content: fmt.Sprintf("Erro ao criar request: %v", err)}
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	client := &http.Client{} // Sem timeout - permite Agent SDK executar pre-flight checks
	resp, err := client.Do(httpReq)
	if err != nil {
		eventChan <- sseEvent{eventType: "error", content: fmt.Sprintf("Erro ao conectar com Python: %v", err)}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		eventChan <- sseEvent{eventType: "error", content: fmt.Sprintf("Erro HTTP %d: %s", resp.StatusCode, string(body))}
		return
	}

	// Ler stream SSE do Python e repassar para o canal
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Erro ao ler stream: %v", err)
			}
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parsear linha SSE
		if strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")

			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				log.Printf("Erro ao parsear JSON: %v", err)
				continue
			}

			// Converter para sseEvent
			eventType, _ := data["type"].(string)

			if eventType == "text" || eventType == "content" {
				if content, ok := data["content"].(string); ok {
					eventChan <- sseEvent{eventType: "text", content: content}
				} else if text, ok := data["text"].(string); ok {
					eventChan <- sseEvent{eventType: "text", content: text}
				}
			} else if eventType == "done" || eventType == "session_created" {
				eventChan <- sseEvent{eventType: "done", sessionID: &sessionID}
			} else if eventType == "error" {
				if errorMsg, ok := data["error"].(string); ok {
					eventChan <- sseEvent{eventType: "error", content: errorMsg}
				}
			}
		}
	}

	log.Printf("✅ Proxy Python finalizado para sessão: %s", sessionID)
}

// appendToSessionFile removida - persistência agora é feita pelo Python SDK

// getCachedSessions retorna sessões do cache se disponíveis e válidas
func getCachedSessions(projectName string) ([]SessionInfo, bool) {
	sessionCache.RLock()
	defer sessionCache.RUnlock()

	entry, exists := sessionCache.data[projectName]
	// Cache válido por 5 minutos
	if !exists || time.Since(entry.timestamp) > 5*time.Minute {
		return nil, false
	}
	return entry.sessions, true
}

// updateSessionCache atualiza o cache de sessões para um projeto
func updateSessionCache(projectName string, sessions []SessionInfo) {
	sessionCache.Lock()
	defer sessionCache.Unlock()

	sessionCache.data[projectName] = cacheEntry{
		sessions:  sessions,
		timestamp: time.Now(),
	}
	log.Printf("📦 Cache atualizado para projeto %s com %d sessões", projectName, len(sessions))
}

// invalidateSessionCache invalida o cache de sessões para um projeto
func invalidateSessionCache(projectName string) {
	sessionCache.Lock()
	defer sessionCache.Unlock()

	delete(sessionCache.data, projectName)
	log.Printf("🗑️  Cache invalidado para projeto: %s", projectName)
}

// getClaudeBaseDir retorna o diretório base do Claude
// Usa a variável de ambiente CLAUDE_BASE_DIR se definida, caso contrário usa ~/.claude
func getClaudeBaseDir() string {
	if baseDir := os.Getenv("CLAUDE_BASE_DIR"); baseDir != "" {
		return baseDir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Em caso de erro crítico, usar fallback mas logar
		log.Printf("ERRO CRÍTICO ao obter home directory: %v - usando fallback", err)
		return "/Users/2a/.claude" // fallback
	}

	return filepath.Join(home, ".claude")
}

// getClaudeProjectsDir retorna o diretório de projetos do Claude
func getClaudeProjectsDir() string {
	return filepath.Join(getClaudeBaseDir(), "projects")
}

// getClaudeProjetosDir retorna o diretório 'projetos' (nome em português)
func getClaudeProjetosDir() string {
	return filepath.Join(getClaudeBaseDir(), "projetos")
}

// validatePath verifica se o path está dentro do base path permitido
func validatePath(path, basePath string) error {
	// Limpar o path
	cleanPath := filepath.Clean(path)

	// Verificar se contém .. ou é absoluto quando não deveria
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("invalid path: contains '..'")
	}

	// Resolver paths absolutos
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return fmt.Errorf("invalid base path: %w", err)
	}

	// Verificar se o path está dentro do base path
	if !strings.HasPrefix(absPath, absBase) {
		return fmt.Errorf("path outside base directory")
	}

	return nil
}

// sanitizeProjectName remove caracteres perigosos do nome do projeto
func sanitizeProjectName(name string) (string, error) {
	// Remover caracteres perigosos
	if strings.ContainsAny(name, "/../\\:*?\"<>|") {
		return "", fmt.Errorf("invalid characters in project name")
	}

	// Limitar tamanho
	if len(name) > 255 {
		return "", fmt.Errorf("project name too long")
	}

	// Não permitir nomes vazios
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("project name cannot be empty")
	}

	return name, nil
}

// sanitizeMessage valida e sanitiza mensagens de chat para prevenir command injection
func sanitizeMessage(msg string) (string, error) {
	// Validar tamanho
	if len(msg) > 10000 {
		return "", fmt.Errorf("message too long (max 10000 chars)")
	}

	// Não permitir mensagens vazias
	if strings.TrimSpace(msg) == "" {
		return "", fmt.Errorf("message cannot be empty")
	}

	// Remover caracteres perigosos para command injection
	dangerous := []string{";", "&", "|", "`", "$", "(", ")", "<", ">", "\n\n\n", "\r"}
	for _, char := range dangerous {
		if strings.Contains(msg, char) {
			return "", fmt.Errorf("message contains invalid characters: %s", char)
		}
	}

	return msg, nil
}

// validateFileOperation verifica segurança de operações com arquivos
func validateFileOperation(filePath string, operation string) error {
	info, err := os.Stat(filePath)
	if err != nil && operation != "create" {
		return fmt.Errorf("file not found")
	}

	if info != nil && info.IsDir() {
		return fmt.Errorf("path is directory, not file")
	}

	ext := filepath.Ext(filePath)
	if ext != ".jsonl" && ext != "" {
		return fmt.Errorf("invalid extension: only .jsonl allowed")
	}

	if info != nil && info.Size() > 100*1024*1024 {
		return fmt.Errorf("file too large: max 100MB")
	}

	return nil
}

// Rate limiter global
var (
	limiters = make(map[string]*rate.Limiter)
	mu       sync.Mutex
)

// getRateLimiter retorna ou cria um rate limiter para um IP
func getRateLimiter(ip string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()

	limiter, exists := limiters[ip]
	if !exists {
		// 2 requisições por segundo, burst de 5
		limiter = rate.NewLimiter(rate.Every(time.Second/2), 5)
		limiters[ip] = limiter
	}

	return limiter
}

// rateLimitMiddleware aplica rate limiting baseado em IP
func rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		limiter := getRateLimiter(ip)

		if !limiter.Allow() {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			log.Printf("⚠️  Rate limit exceeded para IP: %s", ip)
			return
		}

		next(w, r)
	}
}

// authMiddleware verifica API key se configurada
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := os.Getenv("API_KEY")

		// Se API_KEY não configurada, pular autenticação (dev mode)
		if apiKey == "" {
			next(w, r)
			return
		}

		// Verificar header X-API-Key
		providedKey := r.Header.Get("X-API-Key")
		if providedKey != apiKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			log.Printf("⚠️  Acesso não autorizado: %s", r.RemoteAddr)
			return
		}

		next(w, r)
	}
}

func main() {
	log.Println("🚀 Backend Go iniciando na porta 8000...")
	log.Println("✅ Usando CLI do Claude (sem API key necessária)")

	mux := http.NewServeMux()

	// Middleware CORS - Lista explícita de origens permitidas
	allowedOrigins := map[string]bool{
		"http://localhost:3000": true,
		"http://localhost:3001": true,
		"http://localhost:3002": true,
		"http://localhost:3003": true,
	}

	corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Apenas permitir origens na whitelist
			if allowedOrigins[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			// Se origem não está na whitelist, não seta header CORS

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, X-API-Key")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	}

	// Health check
	mux.HandleFunc("GET /health", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{
			"status": "healthy",
			"lang":   "go-hybrid",
			"method": "claude-cli",
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))

	// Endpoint para monitorar sessão em tempo real (raw JSONL)
	mux.HandleFunc("GET /api/live-session", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Buscar a sessão mais recente no diretório padrão
		claudeDir := getClaudeBaseDir()
		var sessionFile string
		var mostRecentTime int64 = 0

		entries, err := os.ReadDir(claudeDir)
		if err != nil {
			http.Error(w, fmt.Sprintf("Erro ao ler diretório: %v", err), http.StatusInternalServerError)
			return
		}

		// Encontrar o arquivo .jsonl mais recente
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".jsonl" {
				info, err := entry.Info()
				if err == nil && info.ModTime().Unix() > mostRecentTime {
					mostRecentTime = info.ModTime().Unix()
					sessionFile = filepath.Join(claudeDir, entry.Name())
				}
			}
		}

		// Se não houver sessão, retornar vazio
		if sessionFile == "" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"lines":   []string{},
				"message": "Nenhuma sessão encontrada",
			})
			return
		}

		// Validar operação de arquivo
		if err := validateFileOperation(sessionFile, "read"); err != nil {
			http.Error(w, fmt.Sprintf("Invalid file operation: %v", err), http.StatusBadRequest)
			return
		}

		// Ler as últimas linhas do arquivo
		file, err := os.Open(sessionFile)
		if err != nil {
			http.Error(w, fmt.Sprintf("Erro ao abrir arquivo: %v", err), http.StatusInternalServerError)
			return
		}
		defer file.Close()

		var lines []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		// Pegar apenas as últimas 20 linhas para preview
		start := 0
		if len(lines) > 20 {
			start = len(lines) - 20
		}

		recentLines := lines[start:]

		json.NewEncoder(w).Encode(map[string]interface{}{
			"lines":        recentLines,
			"total":        len(lines),
			"session_file": filepath.Base(sessionFile),
		})
	}))

	// Listar projetos
	mux.HandleFunc("GET /api/projects", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		projectsPath := getClaudeProjectsDir()
		entries, err := os.ReadDir(projectsPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var projects []map[string]interface{}
		for _, entry := range entries {
			if entry.IsDir() {
				// Contar arquivos .jsonl no diretório
				dirPath := filepath.Join(projectsPath, entry.Name())
				dirEntries, err := os.ReadDir(dirPath)
				if err != nil {
					continue
				}

				sessionCount := 0
				for _, file := range dirEntries {
					if !file.IsDir() && filepath.Ext(file.Name()) == ".jsonl" {
						sessionCount++
					}
				}

				projects = append(projects, map[string]interface{}{
					"name":         entry.Name(),
					"path":         entry.Name(),
					"sessionCount": sessionCount,
				})
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"projects": projects,
		})
	}))

	// Deletar projeto (ambas as pastas)
	mux.HandleFunc("DELETE /api/projects/{projectName}", authMiddleware(corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		projectName := r.PathValue("projectName")

		// Validar e sanitizar nome do projeto
		sanitized, err := sanitizeProjectName(projectName)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid project name: %v", err), http.StatusBadRequest)
			return
		}

		// Extrair nome simples (ex: -Users-2a--claude-diego -> diego)
		simpleName := sanitized
		if strings.HasPrefix(sanitized, "-Users-") {
			parts := strings.Split(sanitized, "-")
			if len(parts) > 0 {
				simpleName = parts[len(parts)-1]
			}
		}

		// Validar e sanitizar nome simples também
		simpleName, err = sanitizeProjectName(simpleName)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid simple name: %v", err), http.StatusBadRequest)
			return
		}

		// Deletar pasta em projects/[nome-completo]
		projectsPath := filepath.Join(getClaudeProjectsDir(), sanitized)

		// Validar que o path está dentro do diretório base
		if err := validatePath(projectsPath, getClaudeProjectsDir()); err != nil {
			http.Error(w, fmt.Sprintf("invalid path: %v", err), http.StatusForbidden)
			return
		}

		err1 := os.RemoveAll(projectsPath)
		if err1 != nil {
			log.Printf("Aviso: %s não encontrado ou erro: %v", projectsPath, err1)
		} else {
			log.Printf("✅ Deletado: %s", projectsPath)
		}

		// Deletar pasta em projetos/[nome-simples]
		simplePath := filepath.Join(getClaudeProjetosDir(), simpleName)

		// Validar que o path está dentro do diretório base
		if err := validatePath(simplePath, getClaudeProjetosDir()); err != nil {
			http.Error(w, fmt.Sprintf("invalid simple path: %v", err), http.StatusForbidden)
			return
		}

		err2 := os.RemoveAll(simplePath)
		if err2 != nil {
			log.Printf("Aviso: %s não encontrado ou erro: %v", simplePath, err2)
		} else {
			log.Printf("✅ Deletado: %s", simplePath)
		}

		log.Printf("Projeto deletado com sucesso")

		// Invalidar cache do projeto deletado
		invalidateSessionCache(sanitized)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Projeto deletado com sucesso",
		})
	})))

	// Listar sessões de um projeto (com cache)
	mux.HandleFunc("GET /api/projects/{projectName}/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		projectName := r.PathValue("projectName")

		// Tentar obter do cache primeiro
		if cachedSessions, found := getCachedSessions(projectName); found {
			log.Printf("✅ Cache hit para projeto: %s", projectName)
			// Converter SessionInfo para map[string]interface{} para manter compatibilidade
			sessions := make([]map[string]interface{}, len(cachedSessions))
			for i, session := range cachedSessions {
				sessions[i] = map[string]interface{}{
					"id":   session.ID,
					"path": session.Path,
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"sessions": sessions,
			})
			return
		}

		log.Printf("❌ Cache miss para projeto: %s - lendo filesystem", projectName)

		// Cache miss - ler do filesystem
		// O projectName já vem formatado do frontend (ex: -Users-2a--claude-projetos-home)
		projectPath := filepath.Join(getClaudeProjectsDir(), projectName)

		log.Printf("🔍 Procurando sessões em: %s", projectPath)

		entries, err := os.ReadDir(projectPath)
		if err != nil {
			log.Printf("❌ Erro ao ler diretório %s: %v", projectPath, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("✅ Encontrados %d arquivos em %s", len(entries), projectPath)

		var sessions []SessionInfo
		for _, file := range entries {
			if !file.IsDir() && filepath.Ext(file.Name()) == ".jsonl" {
				sessionID := strings.TrimSuffix(file.Name(), ".jsonl")
				sessions = append(sessions, SessionInfo{
					ID:   sessionID,
					Path: filepath.Join(projectName, sessionID),
				})
			}
		}

		// Atualizar cache
		updateSessionCache(projectName, sessions)

		// Converter para map[string]interface{} para resposta
		sessionMaps := make([]map[string]interface{}, len(sessions))
		for i, session := range sessions {
			sessionMaps[i] = map[string]interface{}{
				"id":   session.ID,
				"path": session.Path,
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessions": sessionMaps,
		})
	})

	// Obter conteúdo de uma sessão específica
	mux.HandleFunc("GET /api/projects/{projectName}/sessions/{sessionID}", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		projectName := r.PathValue("projectName")
		sessionID := r.PathValue("sessionID")

		sessionFile := filepath.Join(getClaudeProjectsDir(), projectName, sessionID+".jsonl")

		// Validar que o arquivo existe e está no diretório correto
		if err := validatePath(sessionFile, getClaudeProjectsDir()); err != nil {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// Validar operação de arquivo
		if err := validateFileOperation(sessionFile, "read"); err != nil {
			http.Error(w, fmt.Sprintf("Invalid file operation: %v", err), http.StatusBadRequest)
			return
		}

		file, err := os.Open(sessionFile)
		if err != nil {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		defer file.Close()

		var lines []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"lines":        lines,
			"session_file": sessionFile,
			"total":        len(lines),
		})
	}))

	// Deletar uma sessão específica
	mux.HandleFunc("DELETE /api/projects/{projectName}/sessions/{sessionID}", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		projectName := r.PathValue("projectName")
		sessionID := r.PathValue("sessionID")

		sessionFile := filepath.Join(getClaudeProjectsDir(), projectName, sessionID+".jsonl")

		log.Printf("🗑️  Tentando deletar sessão: %s", sessionFile)

		// Validar que o arquivo existe e está no diretório correto
		if err := validatePath(sessionFile, getClaudeProjectsDir()); err != nil {
			log.Printf("❌ Path inválido: %v", err)
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// Validar operação de arquivo
		if err := validateFileOperation(sessionFile, "delete"); err != nil {
			log.Printf("❌ Operação de arquivo inválida: %v", err)
			http.Error(w, fmt.Sprintf("Invalid file operation: %v", err), http.StatusBadRequest)
			return
		}

		// Verificar se o arquivo existe
		if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
			log.Printf("❌ Sessão não encontrada: %s", sessionFile)
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}

		// Deletar o arquivo
		if err := os.Remove(sessionFile); err != nil {
			log.Printf("❌ Erro ao deletar sessão: %v", err)
			http.Error(w, fmt.Sprintf("Error deleting session: %v", err), http.StatusInternalServerError)
			return
		}

		log.Printf("✅ Sessão deletada com sucesso: %s", sessionFile)

		// Invalidar cache do projeto
		invalidateSessionCache(projectName)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Sessão deletada com sucesso",
		})
	}))

	// Limpar histórico da home
	mux.HandleFunc("POST /api/clear-history", authMiddleware(corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Deletar arquivo da sessão home
		homeSessionFile := filepath.Join(getClaudeProjectsDir(), "-Users-2a--claude-projetos-home", "00000000-0000-0000-0000-000000000001.jsonl")

		// Validar operação de arquivo antes de deletar
		if err := validateFileOperation(homeSessionFile, "delete"); err != nil {
			log.Printf("Aviso: validação falhou mas continuando: %v", err)
			// Não retornar erro aqui pois o arquivo pode não existir
		}

		err := os.Remove(homeSessionFile)

		if err != nil && !os.IsNotExist(err) {
			log.Printf("Erro ao deletar histórico: %v", err)
			http.Error(w, "Erro ao limpar histórico", http.StatusInternalServerError)
			return
		}

		log.Printf("✅ Histórico limpo: %s", homeSessionFile)

		// Invalidar cache do projeto home
		invalidateSessionCache("-Users-2a--claude-projetos-home")

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Histórico limpo com sucesso",
		})
	})))

	// isValidUUID verifica se uma string é um UUID válido
	isValidUUID := func(s string) bool {
		// Regex para UUID v4: 8-4-4-4-12 caracteres hexadecimais
		matched, _ := regexp.MatchString(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`, s)
		return matched
	}

	// Fork session - Cria uma ramificação de uma sessão existente
	mux.HandleFunc("POST /api/fork-session", authMiddleware(corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req struct {
			SourceSession string `json:"source_session"`
			ForkSession   string `json:"fork_session"`
			ProjectName   string `json:"project_name"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validar UUIDs
		if !isValidUUID(req.SourceSession) || !isValidUUID(req.ForkSession) {
			http.Error(w, "UUIDs inválidos", http.StatusBadRequest)
			return
		}

		// Validar nome do projeto
		if req.ProjectName == "" {
			http.Error(w, "Nome do projeto obrigatório", http.StatusBadRequest)
			return
		}

		// Construir caminhos
		projectDir := filepath.Join(getClaudeProjectsDir(), req.ProjectName)
		sourcePath := filepath.Join(projectDir, req.SourceSession+".jsonl")
		forkPath := filepath.Join(projectDir, req.ForkSession+".jsonl")

		// Validar que arquivo fonte existe
		if err := validateFileOperation(sourcePath, "read"); err != nil {
			http.Error(w, fmt.Sprintf("Sessão fonte não encontrada: %v", err), http.StatusNotFound)
			return
		}

		// Verificar se fork já existe
		if _, err := os.Stat(forkPath); err == nil {
			http.Error(w, "Fork já existe", http.StatusConflict)
			return
		}

		// Ler arquivo fonte
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			log.Printf("Erro ao ler sessão fonte: %v", err)
			http.Error(w, "Erro ao ler sessão fonte", http.StatusInternalServerError)
			return
		}

		// Criar diretório do projeto se não existir
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			log.Printf("Erro ao criar diretório: %v", err)
			http.Error(w, "Erro ao criar diretório", http.StatusInternalServerError)
			return
		}

		// Escrever arquivo fork
		if err := os.WriteFile(forkPath, data, 0644); err != nil {
			log.Printf("Erro ao criar fork: %v", err)
			http.Error(w, "Erro ao criar fork", http.StatusInternalServerError)
			return
		}

		log.Printf("🔀 Fork criado: %s → %s", req.SourceSession, req.ForkSession)

		// Invalidar cache do projeto
		invalidateSessionCache(req.ProjectName)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         true,
			"fork_session_id": req.ForkSession,
			"source_session_id": req.SourceSession,
			"project_name":    req.ProjectName,
			"fork_path":       forkPath,
		})
	})))

	// Chat endpoint com streaming SSE via CLI (com rate limiting e autenticação)
	mux.HandleFunc("POST /api/chat", authMiddleware(rateLimitMiddleware(corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validar e sanitizar mensagem
		sanitized, err := sanitizeMessage(req.Message)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid message: %v", err), http.StatusBadRequest)
			return
		}

		req.Message = sanitized

		// Configurar SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		ctx := context.Background()

		var sessionID string
		var sessionExists bool
		var projectDir string

		if req.SessionID != nil && *req.SessionID != "" {
			sessionID = *req.SessionID

			if req.ProjectName != nil && *req.ProjectName != "" {
				// Primeiro, tentar encontrar a sessão existente em /projects/ (onde o SDK salva)
				projectsBase := getClaudeProjectsDir()
				existingSessionPath := filepath.Join(projectsBase, *req.ProjectName, sessionID+".jsonl")

				log.Printf("🔍 Procurando sessão em: %s", existingSessionPath)

				if _, err := os.Stat(existingSessionPath); err == nil {
					// Sessão existe em /projects/ - usar esse diretório
					projectDir = filepath.Join(projectsBase, *req.ProjectName)
					sessionExists = true
					log.Printf("✅ Sessão existente encontrada em: %s", existingSessionPath)
				} else {
					log.Printf("❌ Sessão não encontrada (erro: %v), criando nova", err)
					// Sessão não existe - criar novo projeto em /projetos/
					claudeBase := getClaudeProjetosDir()
					projectDir = filepath.Join(claudeBase, *req.ProjectName)

					if err := os.MkdirAll(projectDir, 0755); err != nil {
						msg := SSEMessage{Type: "error", Content: fmt.Sprintf("Erro ao criar diretório do projeto: %v", err)}
						data, _ := json.Marshal(msg)
						fmt.Fprintf(w, "data: %s\n\n", data)
						flusher.Flush()
						return
					}
					sessionExists = false
					log.Printf("📁 Novo projeto criado em: %s", projectDir)
				}
			} else {
				projectsBase := getClaudeProjectsDir()
				dirs, _ := os.ReadDir(projectsBase)

				for _, dir := range dirs {
					if dir.IsDir() {
						projPath := filepath.Join(projectsBase, dir.Name())
						sessionFile := filepath.Join(projPath, sessionID+".jsonl")
						if _, err := os.Stat(sessionFile); err == nil {
							projectDir = projPath
							sessionExists = true
							log.Printf("Sessão encontrada em: %s", projPath)
							break
						}
					}
				}

				if projectDir == "" {
					projectDir = getClaudeBaseDir()
					sessionExists = false
				}
			}
		} else {
			projectDir = getClaudeBaseDir()
			entries, _ := os.ReadDir(projectDir)

			for _, entry := range entries {
				if !entry.IsDir() && filepath.Ext(entry.Name()) == ".jsonl" {
					sessionID = strings.TrimSuffix(entry.Name(), ".jsonl")
					sessionExists = true
					break
				}
			}

			if !sessionExists {
				sessionID = uuid.New().String()
			}
		}

		// Obter ou criar fila para esta sessão
		queue := getOrCreateQueue(sessionID)

		// Criar canal para eventos SSE
		eventChan := make(chan sseEvent, 100)

		// Enfileirar mensagem
		queue.enqueue(queuedMessage{
			message:  req,
			response: eventChan,
			ctx:      ctx,
		})

		// Se não está processando, iniciar processamento da fila
		if !queue.isProcessing() {
			queue.setProcessing(true)
			log.Printf("🚀 Iniciando processamento da fila para sessão: %s", sessionID)
			go processQueue(sessionID, projectDir, sessionExists)
		} else {
			log.Printf("⏳ Sessão %s já está processando, mensagem enfileirada", sessionID)
		}

		// Ler eventos do canal e enviar via SSE
		for event := range eventChan {
			var msg SSEMessage
			switch event.eventType {
			case "text":
				msg = SSEMessage{Type: "text", Content: event.content}
			case "error":
				msg = SSEMessage{Type: "error", Content: event.content}
			case "done":
				msg = SSEMessage{Type: "done", SessionID: event.sessionID}
			}

			data, _ := json.Marshal(msg)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		// Invalidar cache do projeto após criar/atualizar sessão
		if req.ProjectName != nil && *req.ProjectName != "" {
			invalidateSessionCache(*req.ProjectName)
		} else {
			projectsBase := getClaudeProjectsDir()
			dirs, _ := os.ReadDir(projectsBase)
			for _, dir := range dirs {
				if dir.IsDir() {
					invalidateSessionCache(dir.Name())
				}
			}
		}
	}))))

	// CORS
	handler := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:3001", "http://localhost:3002", "http://localhost:3003"},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	}).Handler(mux)

	log.Println("✅ Servidor rodando em http://localhost:8000")
	log.Println("📊 Health: http://localhost:8000/health")

	if err := http.ListenAndServe(":8000", handler); err != nil {
		log.Fatal(err)
	}
}
