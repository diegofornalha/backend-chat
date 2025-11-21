# Backend Chat - Claude Agent SDK via CLI

Backend Python que usa o Claude Agent SDK **sem precisar de ANTHROPIC_API_KEY**.

## ✨ Vantagens

- ✅ **Sem API Key**: Usa sua autenticação do Claude Code CLI
- ✅ **Sem custos extras**: Tokens inclusos no seu plano
- ✅ **Setup simples**: Apenas `pip install` e `python server.py`
- ✅ **Pool de conexões**: 2-10 conexões simultâneas otimizadas
- ✅ **Sessões isoladas**: Múltiplas conversas independentes
- ✅ **Integração Neo4j**: Persistência de conversas
- ✅ **WebSocket**: Streaming em tempo real

## 🚀 Quick Start

```bash
# 1. Certifique-se de estar logado no Claude Code CLI
claude login  # Se ainda não estiver

# 2. Instalar dependências
pip install -r requirements.txt

# 3. Iniciar servidor
python server.py

# Servidor rodando em http://localhost:8000
```

## 📁 Estrutura

```
backend-chat/
├── server.py                    # Servidor FastAPI principal
├── sdk/claude_code_sdk/         # SDK customizado (conecta ao CLI)
├── core/
│   ├── claude_handler.py        # Handler com pool de conexões
│   ├── session_manager.py       # Gerenciador de sessões
│   └── jsonl_monitor.py         # Monitor de arquivos de sessão
├── isolated_session_manager.py  # Sessões protegidas
├── routes/                      # Endpoints da API
├── middleware/                  # Exception handling
├── utils/                       # Logging e utilitários
└── tests/                       # Testes automatizados
```

## 🔌 Endpoints

### HTTP
- `GET /` - Health check
- `POST /chat` - Enviar mensagem
- `GET /neo4j/pending` - Operações Neo4j pendentes

### WebSocket
- `WS /ws` - Conexão bidirecional para streaming

## 🛡️ Sessões Protegidas

Duas sessões dedicadas que nunca são unificadas:
- **Web**: `00000000-0000-0000-0000-000000000001`
- **Terminal**: `4b5f9b35-31b7-4789-88a1-390ecdf21559`

## 🔧 Como Funciona

1. **SDK Customizado** se conecta ao processo Claude Code CLI local
2. **Aproveita autenticação** existente (sem API key)
3. **Pool de conexões** reutiliza clientes (2-10 conexões)
4. **Session Manager** controla contexto e estado
5. **WebSocket** permite streaming bidirecional
6. **Neo4j** persiste conversas (opcional)

## 📊 Pool de Conexões

Configuração otimizada:
- **Mínimo**: 2 conexões
- **Máximo**: 10 conexões
- **Max idade**: 60 minutos
- **Max usos**: 100 por conexão
- **Health check**: A cada 5 minutos

## 🐛 Debug

```python
# Logs contextuais em utils/logging_config.py
logger.info("Mensagem", extra={
    "event": "nome_evento",
    "session_id": "...",
    "custom_field": "..."
})
```

## 📚 Documentação Adicional

- `TESTE_NAVEGACAO.md` - Guia de navegação no código
- `docs/` - Documentação detalhada
- `examples/` - Exemplos de uso

## 🆚 Comparação com packages/

| Aspecto | backend-chat/ | packages/ |
|---------|--------------|-----------|
| API Key | ❌ Não precisa | ✅ Requer |
| E2B | ❌ Não precisa | ✅ Requer |
| Linguagem | Python | TypeScript/Bun |
| Sandbox | Local (CLI) | Remoto (E2B) |
| Setup | Simples | Complexo |
| Custos | Incluso no plano | Pay-per-use |

## 💡 Dica

Para desenvolvimento local, **sempre use backend-chat/**. É mais simples, rápido e não tem custos adicionais.
