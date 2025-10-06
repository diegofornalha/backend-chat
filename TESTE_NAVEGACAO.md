# Script de Teste de Navegação - Chat com Claude SDK

## Objetivo
Testar o fluxo completo de criação de sessões, navegação entre conversas e continuação de conversas existentes. Validar que o sistema gerencia corretamente múltiplas sessões independentes e permite retomar conversas do histórico.

## Pré-requisitos

### Ambiente
- **Servidor backend:** `.venv/bin/python server.py` rodando na porta 8080
- **Servidor frontend:** Rodando na porta 3333 (Live Server ou similar)
- **Neo4j:** Rodando em localhost:7687 (opcional, mas recomendado)
- **DevTools:** Chrome DevTools conectado

### Limpeza Inicial
```bash
# Opcional: limpar sessões antigas para teste limpo
rm -f /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl

# Verificar apenas 1 processo do servidor rodando
lsof -ti:8080 | wc -l  # Deve retornar 1

# Se múltiplos processos, matar todos e reiniciar
pkill -9 -f "python.*server.py"
.venv/bin/python server.py > /tmp/chat-server.log 2>&1 &
```

---

## Roteiro de Testes Completo

### 1️⃣ Criar primeira conversa (Novo Chat)

**URL:** http://localhost:3333/html/index.html

**Preparação via DevTools:**
```javascript
// Limpar localStorage para garantir teste limpo
localStorage.clear();
location.reload();
```

**Após reload, enviar mensagem:**
```javascript
const input = document.getElementById('message-input');
const btn = document.getElementById('send-button');
input.value = 'conversa 1';
btn.click();
```

**Logs esperados do servidor:**
```
📨 Mensagem recebida: {"message":"conversa 1","conversation_id":null}
✨ NOVA SESSÃO CRIADA: conv_id=XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX
🔍 Processando: message=conversa 1..., conv_id=XXXXX, session_id=None, new_session=True
🤖 Iniciando processamento com Claude SDK...
🔧 ClaudeAgentOptions: continue_conversation=False, resume=None
✅ Resposta completa enviada
```

**Verificação:**
```bash
# Um novo arquivo .jsonl foi criado
ls -la /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl

# Arquivo tem 2 linhas (user + assistant)
wc -l /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl
```

**Resultado esperado:**
- ✅ Chat mostra 2 mensagens: "conversa 1" e resposta do Claude
- ✅ Novo arquivo `.jsonl` criado
- ✅ Log confirma `new_session=True` e `continue_conversation=False`
- ✅ Botão "Enviar" habilitado após resposta

---

### 2️⃣ Criar segunda conversa (Novo Chat)

**Ações via DevTools:**
```javascript
// Clicar em "✨ Novo Chat"
document.getElementById('new-chat-btn').click();
```

**Comportamento esperado:**
- Chat limpa completamente (sem mensagens)
- `localStorage.removeItem('claude_chat_history')` executado
- Status muda para "🟡 Conectando..."
- WebSocket fecha e reabre

**Aguardar reload e enviar:**
```javascript
// Aguardar alguns segundos após reload
// Enviar "conversa 2"
const input = document.getElementById('message-input');
const btn = document.getElementById('send-button');
input.value = 'conversa 2';
btn.click();
```

**Logs esperados:**
```
📨 Mensagem recebida: {"message":"conversa 2","conversation_id":null}
✨ NOVA SESSÃO CRIADA: conv_id=YYYYYYYY-YYYY-YYYY-YYYY-YYYYYYYYYYYY
🔍 Processando: message=conversa 2..., conv_id=YYYYY, session_id=None, new_session=True
🔧 ClaudeAgentOptions: continue_conversation=False, resume=None
✅ Resposta completa enviada
```

**Verificação:**
```bash
# Agora devem existir 2 arquivos .jsonl diferentes
ls -lt /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl

# Cada um com 2 linhas
wc -l /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl
```

**Resultado esperado:**
- ✅ 2 arquivos `.jsonl` distintos criados
- ✅ Cada arquivo com 2 linhas (1 conversa completa)
- ✅ Ambos com `new_session=True` nos logs

---

### 3️⃣ Navegar para histórico e verificar

**Ações via DevTools:**
```javascript
// Clicar no link "📁 Histórico"
document.getElementById('history-link').click();
```

**URL após navegação:** http://localhost:3333/html/index_projects.html

**Resultado esperado:**
- ✅ Lista mostra 2 sessões do projeto `backend-chat`
- ✅ Primeira sessão tem badge **"✨ Atual"** (destaque verde)
- ✅ Cada sessão mostra:
  - Nome do projeto
  - Nome do arquivo
  - **2 mensagens**
  - Modelo: `claude-sonnet-4-5-20250929`
  - Data/hora
  - Botão 🗑️ para deletar

**Visual esperado:**
```
┌─────────────────────────────────────┐  ← Borda verde esquerda
│ 🗂️ -Users-2a--factory-backend-chat │
│ XXXXXX.jsonl                        │
│ ✨ Atual  2 mensagens  🗑️           │  ← Badge verde "Atual"
│ 🤖 claude-sonnet-4-5-20250929       │
│ 📅 06/10/2025, 00:00:00             │
└─────────────────────────────────────┘

┌─────────────────────────────────────┐
│ 🗂️ -Users-2a--factory-backend-chat │
│ YYYYYY.jsonl                        │
│ 2 mensagens  🗑️                     │
│ 🤖 claude-sonnet-4-5-20250929       │
│ 📅 06/10/2025, 00:00:00             │
└─────────────────────────────────────┘
```

---

### 4️⃣ Continuar conversa existente (Teste crítico)

**Ações via DevTools:**
```javascript
// Clicar na SEGUNDA sessão (conversa 1 - mais antiga)
const cards = document.querySelectorAll('.session-card');
// Índice pode variar: factory é [0], primeira backend-chat é [1], segunda é [2]
cards[2].click(); // Conversa 1
```

**URL após navegação:**
```
http://localhost:3333/html/session-viewer.html?session_id=XXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX
```

**Tela esperada:**
- ✅ Mostra 2 mensagens antigas da "conversa 1"
- ✅ Campo de input habilitado
- ✅ Botão "Enviar ➤" e "🔄" visíveis
- ✅ WebSocket conectado (status 🟢)

**Continuar a conversa:**
```javascript
// Enviar mensagem para continuar
const input = document.getElementById('message-input');
const btn = document.getElementById('send-button');
input.value = 'continuando conversa 1';
btn.click();
```

**⚠️ LOGS CRÍTICOS - Bug corrigido:**

**Antes da correção (INCORRETO):**
```
📨 Mensagem recebida: {"message":"continuando conversa 1","session_id":"XXXXXX"}
🔍 Processando: session_id=XXXXXX, new_session=True  ← ❌ ERRADO!
🔧 ClaudeAgentOptions: continue_conversation=True, resume=XXXXXX
```
**Problema:** Criava NOVA sessão mesmo com session_id presente

**Depois da correção (CORRETO):**
```
📨 Mensagem recebida: {"message":"continuando conversa 1","session_id":"XXXXXX"}
🔍 Processando: session_id=XXXXXX, new_session=False  ← ✅ CORRETO!
🔧 ClaudeAgentOptions: continue_conversation=False, resume=XXXXXX
```

**Código da correção (server.py:384):**
```python
is_new_session = not received_conv_id and not session_id
# Nova sessão APENAS se NÃO tiver conversation_id E NEM session_id
```

**Código da correção de continue_conversation (server.py:464):**
```python
should_continue = not is_new_session and not session_id
# continue_conversation=True conflita com resume quando há session_id
# Usar apenas para continuar conversa em RAM sem session_id
```

**Verificação após enviar:**
```bash
# Arquivo DEVE ter crescido de 2 para 4 linhas
wc -l /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/XXXXXX.jsonl

# Ver conteúdo adicionado
tail -4 /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/XXXXXX.jsonl
```

**Resultado esperado:**
- ✅ Mensagem "continuando conversa 1" aparece no chat
- ✅ Claude responde mantendo contexto
- ✅ Arquivo `.jsonl` cresce de 2 para 4 linhas
- ✅ **MESMO** arquivo (não cria novo)
- ✅ `sessionId` no .jsonl permanece o mesmo

---

### 5️⃣ Voltar ao histórico e validar contadores

**Ações via DevTools:**
```javascript
// Voltar ao histórico
window.location.href = 'index_projects.html';
```

**Resultado esperado:**
- ✅ Ainda mostra 2 sessões (não criou terceira)
- ✅ Conversa 1 agora mostra **4 mensagens** (cresceu de 2)
- ✅ Conversa 2 permanece com **2 mensagens** (inalterada)
- ✅ Badge "✨ Atual" agora na conversa 1 (foi atualizada por último)

**Ordem esperada das sessões:**
1. Conversa 1 - 4 mensagens - **✨ Atual** (atualizada mais recentemente)
2. Conversa 2 - 2 mensagens (inalterada)

---

## Bugs Identificados e Corrigidos

### Bug 1: Continuar sessão criava novo arquivo

**Problema:**
Ao clicar em uma sessão existente no histórico e enviar mensagem, o sistema criava um NOVO arquivo `.jsonl` ao invés de adicionar ao existente.

**Causa raiz:**
```python
# ANTES (INCORRETO)
is_new_session = not received_conv_id  # Sempre True quando só tinha session_id
```

**Solução:**
```python
# DEPOIS (CORRETO)
is_new_session = not received_conv_id and not session_id
# Nova sessão APENAS se não tiver conversation_id E nem session_id
```

**Evidência da correção:**
```
# Antes: criava arquivo novo
📨 {"session_id":"XXXXX"}
new_session=True ← ERRADO
Resultado: novo arquivo criado

# Depois: adiciona ao existente
📨 {"session_id":"XXXXX"}
new_session=False ← CORRETO
Resultado: mesmo arquivo cresce de 2 para 4 linhas
```

---

### Bug 2: continue_conversation conflitando com resume

**Problema:**
Quando `continue_conversation=True` e `resume=session_id` ao mesmo tempo, o SDK usava a sessão errada.

**Causa raiz:**
```python
# ANTES (CONFLITO)
continue_conversation=not is_new_session  # True quando tinha session_id
resume=session_id  # Ambos ativos simultaneamente
```

**Solução:**
```python
# DEPOIS (SEM CONFLITO)
should_continue = not is_new_session and not session_id
continue_conversation=should_continue  # False quando tem session_id
resume=session_id  # Apenas resume é usado
```

**Evidência:**
```
# Antes: mensagens iam pro arquivo errado
session_id=AAAAA, continue_conversation=True, resume=AAAAA
Resultado: escrevia em arquivo BBBBB (errado)

# Depois: mensagens no arquivo correto
session_id=AAAAA, continue_conversation=False, resume=AAAAA
Resultado: escreve em arquivo AAAAA (correto)
```

---

## Tabela de Comportamento Correto

| Cenário | Origem | conversation_id | session_id | is_new_session | continue_conversation | resume | Resultado |
|---------|--------|----------------|------------|----------------|-----------------------|--------|-----------|
| **Primeira mensagem no index.html** | Novo chat | `null` | `null` | `True` | `False` | `None` | Cria novo .jsonl |
| **Segunda mensagem no mesmo chat** | Index.html | presente | `null` | `False` | `True` | `None` | Adiciona à RAM, cria .jsonl |
| **Abrir sessão do histórico** | Session-viewer | `null` | presente | `False` | `False` | session_id | Continua no mesmo .jsonl |
| **Após clicar "Novo Chat"** | Index.html | `null` | `null` | `True` | `False` | `None` | Cria novo .jsonl |

---

## Melhorias Implementadas

### 1. Log Explícito de Nova Sessão

**Código (server.py:386-387):**
```python
if is_new_session:
    print(f"✨ NOVA SESSÃO CRIADA: conv_id={conversation_id}")
```

**Benefício:** Facilita debug visual para identificar quando novas sessões são criadas.

---

### 2. Limpeza Seletiva do localStorage

**Código (app.js:817):**
```javascript
// ANTES: apagava tudo
localStorage.clear();

// DEPOIS: preserva outras configurações
localStorage.removeItem('claude_chat_history');
```

**Benefício:** Preserva preferências como tema do usuário.

---

### 3. Destaque Visual da Sessão Atual

**Código (index_projects.html:55-57):**
```javascript
const isActive = index === 0; // Primeira sessão (mais recente)
if (isActive) {
    card.classList.add('active-session');
}
```

**CSS (style.css:1447-1460):**
```css
.session-card.active-session {
    border-left: 4px solid #10b981;
    background: linear-gradient(to right, rgba(16, 185, 129, 0.1), transparent);
}

.active-badge {
    background: #10b981;
    color: white;
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
    font-size: 0.75rem;
    font-weight: 600;
}
```

**Benefício:** Usuário identifica visualmente qual foi a última sessão usada.

---

## Comandos de Verificação

### Durante os testes

```bash
# Ver logs em tempo real
tail -f /tmp/chat-server.log

# Contar arquivos criados
ls -1 /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl | wc -l

# Ver detalhes de cada sessão
wc -l /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl

# Buscar logs importantes
grep -E "NOVA SESSÃO|new_session|ClaudeAgentOptions" /tmp/chat-server.log

# Ver último arquivo criado
ls -lt /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl | head -2
```

### Análise de conteúdo

```bash
# Ver primeira linha de cada arquivo (metadados)
for f in /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl; do
    echo "=== $f ==="
    head -1 "$f" | jq -r '.sessionId'
done

# Verificar se sessionId é consistente dentro do arquivo
for f in /Users/2a/.claude/projects/-Users-2a--factory-backend-chat/*.jsonl; do
    echo "=== $f ==="
    cat "$f" | jq -r '.sessionId' | uniq
done
```

---

## Cenários de Falha Comuns

### Falha 1: Múltiplos processos do servidor

**Sintoma:** Conexão WebSocket fecha aleatoriamente, comportamento inconsistente.

**Diagnóstico:**
```bash
lsof -ti:8080 | wc -l  # Se > 1, há múltiplos processos
```

**Solução:**
```bash
pkill -9 -f "python.*server.py"
.venv/bin/python server.py > /tmp/chat-server.log 2>&1 &
```

---

### Falha 2: localStorage não limpa

**Sintoma:** Após clicar "Novo Chat", mensagens antigas ainda aparecem.

**Diagnóstico via DevTools:**
```javascript
localStorage.getItem('claude_chat_history')
// Se retornar algo, não foi limpo
```

**Solução:**
```javascript
localStorage.clear();
location.reload();
```

---

### Falha 3: Mensagem vai para arquivo errado

**Sintoma:** Continuar conversa 1 adiciona mensagens na conversa 2.

**Diagnóstico nos logs:**
```bash
tail -50 /tmp/chat-server.log | grep -E "session_id|continue_conversation|resume"
```

**O que buscar:**
- ✅ **CORRETO:** `session_id=XXXXX, continue_conversation=False, resume=XXXXX`
- ❌ **INCORRETO:** `session_id=XXXXX, continue_conversation=True, resume=XXXXX`

**Solução aplicada (server.py:464):**
```python
should_continue = not is_new_session and not session_id
continue_conversation=should_continue
```

---

## Checklist Final de Validação

Após completar todos os passos:

- [ ] 2 arquivos `.jsonl` distintos criados
- [ ] Conversa 1 tem **4 mensagens** (2 iniciais + 2 de continuação)
- [ ] Conversa 2 tem **2 mensagens** (inalterada)
- [ ] Badge "✨ Atual" na conversa que foi atualizada por último
- [ ] Logs mostram `new_session=True` para conversas novas
- [ ] Logs mostram `new_session=False` para continuar sessão
- [ ] Logs mostram `continue_conversation=False` quando há `session_id`
- [ ] Nenhum arquivo `.jsonl` extra foi criado (apenas os 2 esperados)
- [ ] Botões 🗑️ funcionando (deletam o arquivo fisicamente)
- [ ] "Claude está processando..." aparece durante streaming
- [ ] Botão 🔄 recarrega a página corretamente

---

## Funcionalidades do Sistema

### Interface Principal (index.html)
- ✅ Chat com streaming em tempo real
- ✅ Botão "✨ Novo Chat" - limpa e cria nova sessão
- ✅ Botão "📁 Histórico" - navega para lista de sessões
- ✅ Indicador "Claude está processando..." com timer
- ✅ Mensagens com markdown e syntax highlighting
- ✅ Botão copiar código
- ✅ Debug Visual (Ctrl+D)

### Visualizador de Sessão (session-viewer.html)
- ✅ Carrega sessão `.jsonl` existente
- ✅ Permite continuar conversa
- ✅ Seleção múltipla de mensagens
- ✅ Exclusão individual ou em lote
- ✅ Botão 🔄 atualizar
- ✅ Indicador "Claude está processando..." com timer
- ✅ Checkboxes limpos (sem texto "Selecionar")

### Lista de Projetos (index_projects.html)
- ✅ Lista todas as sessões por projeto
- ✅ Badge "✨ Atual" na sessão mais recente
- ✅ Destaque visual (borda verde, background gradient)
- ✅ Contador de mensagens atualizado
- ✅ Botão 🗑️ deleta sem popup de confirmação
- ✅ Erros apenas no Debug Visual (sem popups)

### Backend (server.py)
- ✅ Endpoint `GET /sessions` - lista sessões
- ✅ Endpoint `GET /sessions/{id}` - carrega sessão
- ✅ Endpoint `DELETE /sessions/{id}` - deleta arquivo .jsonl
- ✅ Endpoint `DELETE /sessions/{id}/messages` - remove mensagem específica
- ✅ WebSocket `/ws/chat` com streaming
- ✅ Gerenciamento correto de `is_new_session`
- ✅ Logs explícitos com emojis para debug
- ✅ Suporte a `continue_conversation` e `resume`

---

## Logs de Exemplo de Teste Bem-Sucedido

```
📨 Mensagem recebida: {"message":"conversa 1","conversation_id":null}
✨ NOVA SESSÃO CRIADA: conv_id=abc123...
🔍 Processando: message=conversa 1..., conv_id=abc123, session_id=None, new_session=True
🔧 ClaudeAgentOptions: continue_conversation=False, resume=None
✅ Resposta completa enviada

📨 Mensagem recebida: {"message":"conversa 2","conversation_id":null}
✨ NOVA SESSÃO CRIADA: conv_id=def456...
🔍 Processando: message=conversa 2..., conv_id=def456, session_id=None, new_session=True
🔧 ClaudeAgentOptions: continue_conversation=False, resume=None
✅ Resposta completa enviada

📨 Mensagem recebida: {"message":"continuando conversa 1","session_id":"abc123"}
🔍 Processando: message=continuando..., conv_id=xyz789, session_id=abc123, new_session=False
🔧 ClaudeAgentOptions: continue_conversation=False, resume=abc123
✅ Resposta completa enviada
```

---

## Referências Técnicas

### Claude SDK - ClaudeAgentOptions

**Parâmetros principais:**
- `continue_conversation` (bool): Se True, mantém contexto entre mensagens na mesma sessão em memória
- `resume` (str|None): ID de sessão .jsonl para retomar do disco
- `permission_mode`: "bypassPermissions" para ambiente controlado
- `model`: "claude-sonnet-4-5"
- `max_turns`: Limite de turnos de conversação

**Regras de uso:**
1. **Nova conversa:** `continue_conversation=False, resume=None`
2. **Continuar em RAM:** `continue_conversation=True, resume=None`
3. **Retomar do disco:** `continue_conversation=False, resume=session_id`

**Conflito evitado:**
Não usar `continue_conversation=True` junto com `resume=session_id` - escolher um ou outro.

---

## Documentação Adicional

- 📁 Sessões JSONL: `~/.claude/projects/-Users-2a--factory-backend-chat/`
- 📊 Logs do servidor: `/tmp/chat-server.log`
- 🌐 Frontend: http://localhost:3333/html/
- 🔌 Backend API: http://localhost:8080/docs
- 🧠 Neo4j: http://localhost:7474 (Browser)

---

## Próximos Passos Sugeridos

1. **Registrar no Neo4j:** Documentar a solução dos bugs no banco de conhecimento
2. **Testes de carga:** Validar com 10+ sessões simultâneas
3. **Implementar busca:** Filtrar sessões por conteúdo ou data
4. **Exportação:** Permitir exportar sessão como Markdown
5. **Atalhos de teclado:** Ctrl+N para novo chat, Ctrl+H para histórico
