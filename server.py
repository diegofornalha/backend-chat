#!/usr/bin/env python3
"""Backend do chat com Claude SDK - Streaming real."""

import sys
from pathlib import Path
from datetime import datetime
from typing import AsyncIterator, Optional, List
import json
import uuid

# Adicionar claude-agent-sdk ao path
sdk_path = Path("/Users/2a/Desktop/youtube_clickbait/claude-agent-sdk-python")
sys.path.insert(0, str(sdk_path))

from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel

from claude_agent_sdk import (
    ClaudeSDKClient,
    ClaudeAgentOptions,
    AssistantMessage,
    TextBlock,
    ThinkingBlock,
    ToolUseBlock,
    ToolResultBlock,
    ResultMessage,
)

from code_runner import get_code_runner

app = FastAPI(title="Claude Chat API", version="1.0.0")

# CORS habilitado para permitir Live Server
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],  # Em produção, especificar domínios
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Armazenamento de conversas (em memória)
conversations = {}

# Sessões ativas do Claude (mantém contexto)
active_sessions = {}


class Message(BaseModel):
    """Mensagem do chat."""
    role: str
    content: str
    timestamp: str
    thinking: Optional[str] = None


class Conversation(BaseModel):
    """Conversa completa."""
    id: str
    messages: List[Message]
    created_at: str


class ChatRequest(BaseModel):
    """Request de mensagem."""
    message: str
    conversation_id: Optional[str] = None
    session_id: Optional[str] = None  # ID da sessão Claude SDK para resume


class CodeExecutionRequest(BaseModel):
    """Request para executar código."""
    code: str
    language: str = "python"


class DeleteMessageRequest(BaseModel):
    """Request para remover mensagem de sessão JSONL."""
    message_id: str | None = None
    line_index: int | None = None


@app.get("/")
async def root():
    """Health check."""
    return {"status": "ok", "service": "Claude Chat API"}


# Queue de operações Neo4j pendentes
neo4j_operations_queue = []


@app.get("/neo4j/pending")
async def get_pending_neo4j_operations():
    """Retorna operações Neo4j pendentes para execução externa."""
    return {
        "operations": neo4j_operations_queue,
        "count": len(neo4j_operations_queue)
    }


@app.post("/neo4j/mark_processed")
async def mark_neo4j_operations_processed(operation_ids: List[int]):
    """Marca operações como processadas."""
    global neo4j_operations_queue

    # Remover operações processadas
    neo4j_operations_queue = [
        op for i, op in enumerate(neo4j_operations_queue)
        if i not in operation_ids
    ]

    return {"success": True, "remaining": len(neo4j_operations_queue)}


@app.get("/conversations")
async def list_conversations():
    """Lista todas as conversas."""
    return {
        "conversations": [
            {
                "id": conv_id,
                "message_count": len(conv.messages),
                "created_at": conv.created_at,
                "last_message": conv.messages[-1].content[:100] if conv.messages else ""
            }
            for conv_id, conv in conversations.items()
        ]
    }


@app.get("/conversations/{conversation_id}")
async def get_conversation(conversation_id: str):
    """Retorna uma conversa específica."""
    if conversation_id not in conversations:
        return {"error": "Conversation not found"}, 404

    return conversations[conversation_id]


def find_session_file(session_id: str) -> Path | None:
    """Localiza arquivo JSONL correspondente ao session_id."""
    projects_path = Path.home() / ".claude" / "projects"

    for jsonl_file in projects_path.rglob("*.jsonl"):
        if session_id in jsonl_file.name:
            return jsonl_file

    return None


@app.get("/sessions")
async def list_sessions():
    """Lista todas as sessões .jsonl disponíveis."""
    projects_path = Path.home() / ".claude" / "projects"
    sessions = []

    for jsonl_file in projects_path.rglob("*.jsonl"):
        try:
            # Ler primeira e última linha para metadata
            with open(jsonl_file, 'r') as f:
                lines = f.readlines()
                if not lines:
                    continue

                first = json.loads(lines[0].strip())
                last = json.loads(lines[-1].strip()) if len(lines) > 1 else first

                # Usar sessionId do evento, ou extrair do nome do arquivo
                session_id = first.get("sessionId")
                if not session_id:
                    session_id = jsonl_file.stem  # Nome sem extensão

                sessions.append({
                    "session_id": session_id,
                    "file": str(jsonl_file),
                    "file_name": jsonl_file.name,
                    "message_count": len(lines),
                    "created_at": first.get("timestamp", ""),
                    "updated_at": last.get("timestamp", ""),
                    "model": last.get("message", {}).get("model", "unknown") if last.get("type") == "assistant" else "unknown"
                })
        except:
            pass

    sessions.sort(key=lambda x: x["updated_at"], reverse=True)
    return {"sessions": sessions, "count": len(sessions)}


@app.get("/sessions/{session_id}")
async def get_session(session_id: str):
    """Retorna sessão .jsonl do Claude SDK."""
    jsonl_file = find_session_file(session_id)

    if not jsonl_file:
        return {"error": "Session not found"}, 404

    messages = []
    with open(jsonl_file, 'r') as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    messages.append(json.loads(line))
                except:
                    pass

    return {
        "session_id": session_id,
        "file": str(jsonl_file),
        "messages": messages,
        "count": len(messages)
    }


@app.delete("/sessions/{session_id}")
async def delete_session(session_id: str):
    """Remove completamente uma sessão (arquivo JSONL)."""
    jsonl_file = find_session_file(session_id)
    if not jsonl_file:
        return {"error": "Session not found"}, 404

    try:
        jsonl_file.unlink()
        return {
            "success": True,
            "session_id": session_id,
            "file": str(jsonl_file)
        }
    except Exception as e:
        return {"error": f"Falha ao remover sessão: {str(e)}"}, 500


@app.delete("/sessions/{session_id}/messages")
async def delete_session_message(session_id: str, request: DeleteMessageRequest):
    """Remove uma mensagem específica do arquivo JSONL da sessão."""
    if not request.message_id and request.line_index is None:
        return {"error": "message_id ou line_index são obrigatórios"}, 400

    jsonl_file = find_session_file(session_id)
    if not jsonl_file:
        return {"error": "Session not found"}, 404

    kept_lines: list[str] = []
    removed_entries: list[dict] = []

    target_indices: set[int] = set()
    if request.line_index is not None and request.line_index >= 0:
        target_indices.add(request.line_index)

    with open(jsonl_file, 'r', encoding='utf-8') as source:
        for idx, raw_line in enumerate(source):
            stripped = raw_line.strip()

            if not stripped:
                if idx not in target_indices:
                    kept_lines.append(raw_line)
                continue

            try:
                data = json.loads(stripped)
            except json.JSONDecodeError:
                if idx not in target_indices:
                    kept_lines.append(raw_line)
                continue

            match = False

            if idx in target_indices:
                match = True

            if request.message_id and not match:
                candidates = [
                    data.get("messageId"),
                    data.get("id"),
                    data.get("uuid"),
                ]

                msg = data.get("message")
                if isinstance(msg, dict):
                    candidates.extend([
                        msg.get("id"),
                        msg.get("messageId"),
                    ])

                if request.message_id in [c for c in candidates if c]:
                    match = True

            if match:
                removed_entries.append(data)
            else:
                kept_lines.append(raw_line)

    if not removed_entries:
        return {"error": "Mensagem não encontrada"}, 404

    temp_path = jsonl_file.with_suffix(jsonl_file.suffix + ".tmp")
    with open(temp_path, 'w', encoding='utf-8') as target:
        target.writelines(kept_lines)

    temp_path.replace(jsonl_file)

    return {
        "session_id": session_id,
        "removed_count": len(removed_entries),
        "removed_ids": [
            entry.get("messageId")
            or entry.get("id")
            or entry.get("uuid")
            or (entry.get("message", {}) if isinstance(entry.get("message"), dict) else {}).get("id")
        for entry in removed_entries
        ],
        "remaining_messages": len(kept_lines)
    }


class DeleteMessageRequest(BaseModel):
    """Request para deletar mensagem."""
    message_id: Optional[str] = None
    line_index: Optional[int] = None


@app.delete("/sessions/{session_id}/messages")
async def delete_session_message(session_id: str, request: DeleteMessageRequest):
    """Remove mensagem específica de uma sessão .jsonl."""
    from pathlib import Path

    # Procurar arquivo .jsonl
    projects_path = Path.home() / ".claude" / "projects"

    for jsonl_file in projects_path.rglob("*.jsonl"):
        if session_id in jsonl_file.name:
            try:
                # Ler todas as linhas
                with open(jsonl_file, 'r') as f:
                    lines = f.readlines()

                # Encontrar linha a remover
                line_to_remove = None

                if request.line_index is not None and 0 <= request.line_index < len(lines):
                    line_to_remove = request.line_index
                elif request.message_id:
                    for i, line in enumerate(lines):
                        try:
                            data = json.loads(line.strip())
                            if data.get("id") == request.message_id or data.get("messageId") == request.message_id:
                                line_to_remove = i
                                break
                        except:
                            continue

                if line_to_remove is None:
                    return {"error": "Message not found"}, 404

                # Remover linha
                lines.pop(line_to_remove)

                # Reescrever arquivo
                with open(jsonl_file, 'w') as f:
                    f.writelines(lines)

                return {
                    "success": True,
                    "removed_index": line_to_remove,
                    "remaining_count": len(lines)
                }

            except Exception as e:
                return {"error": f"Failed to delete message: {str(e)}"}, 500

    return {"error": "Session not found"}, 404


@app.get("/conversations/{conversation_id}/export")
async def export_conversation(conversation_id: str):
    """Exporta conversa em formato Markdown."""
    if conversation_id not in conversations:
        return {"error": "Conversation not found"}, 404

    conv = conversations[conversation_id]

    # Gerar markdown
    md_content = f"""# 💬 Conversa com Claude

**Data:** {conv.created_at}
**ID:** {conversation_id}
**Mensagens:** {len(conv.messages)}

---

"""

    for i, msg in enumerate(conv.messages, 1):
        role_emoji = "👤" if msg.role == "user" else "🤖"
        role_name = "Você" if msg.role == "user" else "Claude"

        md_content += f"## {role_emoji} {role_name} ({msg.timestamp})\n\n"
        md_content += f"{msg.content}\n\n"

        if msg.thinking:
            md_content += f"*💭 Pensamento: {msg.thinking}*\n\n"

        md_content += "---\n\n"

    return {
        "markdown": md_content,
        "filename": f"conversa_{conversation_id[:8]}.md"
    }


@app.post("/execute/code")
async def execute_code(request: CodeExecutionRequest):
    """Executa código Python de forma segura."""
    if request.language != "python":
        return {"error": "Apenas Python é suportado por enquanto"}, 400

    runner = get_code_runner()
    result = runner.run(request.code)

    return result


@app.websocket("/ws/chat")
async def websocket_chat(websocket: WebSocket):
    """WebSocket para chat com streaming."""
    await websocket.accept()

    try:
        while True:
            # Receber mensagem do cliente
            data = await websocket.receive_text()
            request = json.loads(data)
            print(f"📨 Mensagem recebida: {data}")

            message = request.get("message", "")
            received_conv_id = request.get("conversation_id")
            conversation_id = received_conv_id if received_conv_id else str(uuid.uuid4())
            session_id = request.get("session_id")  # ID da sessão Claude SDK (opcional)
            is_new_session = not received_conv_id and not session_id  # Nova sessão apenas se não tiver conversation_id E nem session_id

            if is_new_session:
                print(f"✨ NOVA SESSÃO CRIADA: conv_id={conversation_id}")

            print(f"🔍 Processando: message={message[:50]}..., conv_id={conversation_id}, session_id={session_id}, new_session={is_new_session}")

            # Criar conversa se não existir
            if conversation_id not in conversations:
                conversations[conversation_id] = Conversation(
                    id=conversation_id,
                    messages=[],
                    created_at=datetime.now().isoformat()
                )

            # Adicionar mensagem do usuário
            user_message = Message(
                role="user",
                content=message,
                timestamp=datetime.now().isoformat()
            )
            conversations[conversation_id].messages.append(user_message)

            # Enviar confirmação
            await websocket.send_json({
                "type": "user_message_saved",
                "conversation_id": conversation_id
            })

            # Processar com Claude SDK (passar conversation_id, session_id e is_new_session)
            try:
                print(f"🤖 Iniciando processamento com Claude SDK...")
                async for chunk in process_with_claude(message, conversation_id, session_id, is_new_session):
                    await websocket.send_json(chunk)

                    # Salvar mensagem do assistant
                    if chunk.get("type") == "result":
                        assistant_message = Message(
                            role="assistant",
                            content=chunk.get("content", ""),
                            timestamp=datetime.now().isoformat(),
                            thinking=chunk.get("thinking")
                        )
                        conversations[conversation_id].messages.append(assistant_message)
                        print(f"✅ Resposta completa enviada")

            except Exception as e:
                print(f"❌ Erro no processamento: {e}")
                import traceback
                traceback.print_exc()
                await websocket.send_json({
                    "type": "error",
                    "error": str(e)
                })

    except WebSocketDisconnect:
        print("Cliente desconectado")


async def process_with_claude(message: str, conversation_id: str | None = None, session_id: str | None = None, is_new_session: bool = False) -> AsyncIterator[dict]:
    """Processa mensagem com Claude SDK e retorna chunks.

    Args:
        message: Mensagem do usuário
        conversation_id: ID da conversa RAM (mantém contexto se fornecido)
        session_id: ID da sessão Claude SDK (.jsonl) - sobrescreve conversation_id se fornecido
        is_new_session: Se True, força criação de nova sessão sem resume
    """

    # Se session_id foi fornecido, usar ele para resume (sessão .jsonl persistente)
    # Caso contrário, usar conversation_id (memória RAM)
    resume_id = session_id if session_id else conversation_id

    # Só resume se NÃO for nova sessão E (há histórico para conversation_id OU tem session_id)
    conversation = conversations.get(conversation_id) if conversation_id else None
    has_history = not is_new_session and ((conversation and len(conversation.messages) > 1) or bool(session_id))

    resume_value = resume_id if has_history else None
    # continue_conversation=True conflita com resume quando há session_id
    # Usar apenas quando continuar conversa em RAM sem session_id
    should_continue = not is_new_session and not session_id

    print(f"🔧 ClaudeAgentOptions: continue_conversation={should_continue}, resume={resume_value}")

    options = ClaudeAgentOptions(
        model="claude-haiku-4-5-20251001",
        max_turns=10,
        permission_mode="bypassPermissions",
        continue_conversation=should_continue,
        resume=resume_value
    )

    full_content = ""
    thinking_content = ""

    tool_names: dict[str, str] = {}

    try:
        async with ClaudeSDKClient(options=options) as client:
            await client.query(message)

            async for msg in client.receive_response():
                if isinstance(msg, AssistantMessage):
                    for block in msg.content:
                        if isinstance(block, TextBlock):
                            # Enviar chunk de texto
                            full_content += block.text

                            yield {
                                "type": "text_chunk",
                                "content": block.text,
                                "full_content": full_content
                            }

                        elif isinstance(block, ThinkingBlock):
                            # Enviar pensamento
                            thinking_content += block.thinking

                            yield {
                                "type": "thinking",
                                "content": block.thinking
                            }

                        elif isinstance(block, ToolUseBlock):
                            tool_names[block.id] = block.name

                            yield {
                                "type": "tool_start",
                                "tool": block.name,
                                "tool_use_id": block.id,
                                "input": block.input,
                            }

                        elif isinstance(block, ToolResultBlock):
                            tool_name = tool_names.get(block.tool_use_id, "Ferramenta")

                            if isinstance(block.content, list):
                                try:
                                    content_text = json.dumps(block.content, ensure_ascii=False, indent=2)
                                except Exception:
                                    content_text = str(block.content)
                            else:
                                content_text = block.content or ""

                            yield {
                                "type": "tool_result",
                                "tool": tool_name,
                                "tool_use_id": block.tool_use_id,
                                "content": content_text,
                                "is_error": block.is_error,
                            }

                elif isinstance(msg, ResultMessage):
                    # Enviar resultado final
                    result_data = {
                        "type": "result",
                        "content": full_content,
                        "thinking": thinking_content if thinking_content else None,
                        "cost": msg.total_cost_usd,
                        "duration_ms": msg.duration_ms,
                        "num_turns": msg.num_turns,
                        "is_error": msg.is_error
                    }

                    # Enfileirar aprendizado no Neo4j
                    neo4j_operations_queue.append({
                        "tool": "mcp__neo4j-memory__learn_from_result",
                        "params": {
                            "task": f"Chat response generated",
                            "result": f"{msg.num_turns} turns, {msg.duration_ms}ms, ${msg.total_cost_usd:.4f}",
                            "success": not msg.is_error,
                            "category": "chat_interaction"
                        },
                        "timestamp": datetime.now().isoformat()
                    })

                    yield result_data

    except Exception as e:
        yield {
            "type": "error",
            "error": str(e)
        }


if __name__ == "__main__":
    import uvicorn

    print("🚀 Iniciando Claude Chat Server...")
    print("📡 WebSocket: ws://localhost:8080/ws/chat")
    print("📊 API Docs: http://localhost:8080/docs")

    uvicorn.run(app, host="0.0.0.0", port=8080, log_level="info")
