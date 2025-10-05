# API de Deleção de Sessões

## Endpoints Disponíveis

### 1. Deletar UMA sessão específica
```bash
DELETE http://localhost:8000/api/projects/{projectName}/sessions/{sessionID}
```

**Exemplo:**
```bash
curl -X DELETE http://localhost:8000/api/projects/-Users-2a--factory/67e137a4-20c0-4d20-a5c6-286372bce5cf
```

**Resposta de sucesso:**
```json
{
  "success": true,
  "message": "Sessão deletada com sucesso"
}
```

---

### 2. Deletar TODAS as sessões de um projeto
```bash
DELETE http://localhost:8000/api/projects/{projectName}/sessions
```

**Exemplo:**
```bash
curl -X DELETE http://localhost:8000/api/projects/-Users-2a--factory/sessions
```

**Resposta de sucesso:**
```json
{
  "success": true,
  "deleted_count": 5,
  "message": "5 sessão(ões) deletada(s) com sucesso"
}
```

**Resposta com erros parciais:**
```json
{
  "success": true,
  "deleted_count": 3,
  "message": "3 sessão(ões) deletada(s), 2 erro(s)",
  "errors": [
    "invalid.jsonl: file too large: max 100MB",
    "corrupted.jsonl: invalid extension: only .jsonl allowed"
  ]
}
```

---

### 3. Deletar projeto completo (projeto + todas sessões)
```bash
DELETE http://localhost:8000/api/projects/{projectName}
```

**Exemplo:**
```bash
curl -X DELETE http://localhost:8000/api/projects/-Users-2a--factory
```

**Resposta de sucesso:**
```json
{
  "success": true,
  "message": "Projeto deletado com sucesso"
}
```

---

### 4. Deletar mensagens específicas de uma sessão (Python Backend)
```bash
DELETE http://localhost:8080/sessions/{session_id}/messages
```

**Body:**
```json
{
  "message_id": "msg-abc123",
  "line_index": 5
}
```

**Exemplo:**
```bash
curl -X DELETE http://localhost:8080/sessions/67e137a4-20c0-4d20-a5c6-286372bce5cf/messages \
  -H "Content-Type: application/json" \
  -d '{"line_index": 5}'
```

**Resposta:**
```json
{
  "session_id": "67e137a4-20c0-4d20-a5c6-286372bce5cf",
  "removed_count": 1,
  "removed_ids": ["msg-abc123"],
  "remaining_messages": 42
}
```

---

## Integração com Frontend

### JavaScript/Fetch

```javascript
// Deletar uma sessão
async function deleteSession(projectName, sessionID) {
  const response = await fetch(
    `http://localhost:8000/api/projects/${projectName}/sessions/${sessionID}`,
    { method: 'DELETE' }
  );
  return await response.json();
}

// Deletar todas as sessões de um projeto
async function deleteAllSessions(projectName) {
  const response = await fetch(
    `http://localhost:8000/api/projects/${projectName}/sessions`,
    { method: 'DELETE' }
  );
  return await response.json();
}

// Deletar projeto completo
async function deleteProject(projectName) {
  const response = await fetch(
    `http://localhost:8000/api/projects/${projectName}`,
    { method: 'DELETE' }
  );
  return await response.json();
}

// Deletar mensagem específica
async function deleteMessage(sessionID, lineIndex) {
  const response = await fetch(
    `http://localhost:8080/sessions/${sessionID}/messages`,
    {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ line_index: lineIndex })
    }
  );
  return await response.json();
}
```

### Exemplo de UI

```javascript
// Botão "Deletar Todas as Sessões"
document.getElementById('delete-all-btn').addEventListener('click', async () => {
  const projectName = '-Users-2a--factory';

  const result = await deleteAllSessions(projectName);

  if (result.success) {
    console.log(`✅ ${result.deleted_count} sessões deletadas`);
    // Recarregar lista de sessões
    location.reload();
  } else {
    console.error('❌ Erro ao deletar sessões');
  }
});

// Botão "Deletar Esta Sessão"
document.getElementById('delete-session-btn').addEventListener('click', async () => {
  const projectName = '-Users-2a--factory';
  const sessionID = '67e137a4-20c0-4d20-a5c6-286372bce5cf';

  const result = await deleteSession(projectName, sessionID);

  if (result.success) {
    console.log('✅ Sessão deletada');
    window.location.href = '/html/index_projects.html';
  } else {
    console.error('❌ Erro ao deletar sessão');
  }
});
```

---

## Validações de Segurança

Todos os endpoints têm validações:

1. **Path Traversal**: Bloqueia `../` e caminhos fora do diretório base
2. **Extensão de arquivo**: Apenas `.jsonl` permitido
3. **Tamanho de arquivo**: Máximo 100MB
4. **CORS**: Apenas origens whitelisted (`localhost:3000-3003, 3333`)
5. **Rate Limiting**: 2 req/s por IP (máximo burst de 5)

---

## Códigos de Erro

| Código | Significado |
|--------|-------------|
| 200 | Sucesso |
| 400 | Requisição inválida (path/nome inválido) |
| 403 | Proibido (path fora do diretório permitido) |
| 404 | Sessão/projeto não encontrado |
| 429 | Rate limit excedido |
| 500 | Erro interno do servidor |

---

## Fluxo Recomendado

```
┌─────────────────────────────────────────┐
│  Frontend solicita deleção              │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│  Confirmação do usuário (dialog)        │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│  DELETE request para backend            │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│  Backend valida e deleta arquivo(s)     │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│  Cache invalidado automaticamente       │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│  Frontend atualiza UI (reload/redirect) │
└─────────────────────────────────────────┘
```
