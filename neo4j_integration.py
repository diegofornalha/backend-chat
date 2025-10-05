"""Integração com Neo4j para salvar conversas e aprendizados."""

import sys
from pathlib import Path
from datetime import datetime
from typing import Any

# Importar Neo4jClient do projeto principal
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "src"))
from neo4j_client import Neo4jClient


class ChatNeo4jIntegration:
    """Integra chat com Neo4j para aprendizado contínuo."""

    def __init__(self):
        self.neo4j = Neo4jClient()
        self.operations_queue = []

    async def salvar_conversa(
        self,
        conversation_id: str,
        messages: list[dict],
        total_cost: float | None = None
    ) -> list[dict]:
        """Salva conversa completa no Neo4j.

        Args:
            conversation_id: ID da conversa
            messages: Lista de mensagens
            total_cost: Custo total da conversa

        Returns:
            Lista de operações Neo4j preparadas
        """
        operations = []

        # 1. Criar memória da conversa
        conversation_mem = self.neo4j.criar_aprendizado(
            task=f"Chat conversation {conversation_id[:8]}",
            result=f"{len(messages)} messages exchanged, cost: ${total_cost or 0:.4f}",
            success=True,
            category="chat_conversation"
        )
        operations.append(conversation_mem)

        # 2. Analisar conteúdo das mensagens
        user_messages = [m for m in messages if m.get("role") == "user"]
        assistant_messages = [m for m in messages if m.get("role") == "assistant"]

        # 3. Detectar tópicos discutidos
        topics = self._extract_topics(user_messages + assistant_messages)

        for topic in topics:
            topic_mem = self.neo4j.criar_memoria_topico(topic)
            operations.append(topic_mem)

        # 4. Detectar padrões de código gerado
        code_patterns = self._extract_code_patterns(assistant_messages)

        for pattern in code_patterns:
            pattern_mem = self.neo4j.criar_memoria_pattern(
                pattern_name=pattern["name"],
                description=pattern["description"],
                code_snippet=pattern.get("code", ""),
                success_rate=1.0
            )
            operations.append(pattern_mem)

        return operations

    async def salvar_metrics(
        self,
        conversation_id: str,
        response_time_ms: int,
        chunks_count: int,
        cost: float
    ) -> dict:
        """Salva métricas de performance."""
        return self.neo4j.criar_aprendizado(
            task=f"Chat response metrics {conversation_id[:8]}",
            result=f"{response_time_ms}ms, {chunks_count} chunks, ${cost:.4f}",
            success=True,
            category="chat_performance"
        )

    def _extract_topics(self, messages: list[dict]) -> list[str]:
        """Extrai tópicos das mensagens."""
        topics = set()

        keywords = [
            "python", "javascript", "api", "rest", "fastapi", "async",
            "websocket", "streaming", "claude", "sdk", "agent",
            "docker", "database", "neo4j", "frontend", "backend"
        ]

        for msg in messages:
            content = msg.get("content", "").lower()
            for keyword in keywords:
                if keyword in content:
                    topics.add(keyword)

        return list(topics)[:5]  # Top 5

    def _extract_code_patterns(self, messages: list[dict]) -> list[dict]:
        """Extrai padrões de código das mensagens do assistant."""
        patterns = []

        for msg in messages:
            content = msg.get("content", "")

            # Detectar FastAPI
            if "FastAPI" in content or "fastapi" in content.lower():
                patterns.append({
                    "name": "FastAPI Usage Pattern",
                    "description": "Código gerado usando FastAPI framework",
                    "code": self._extract_code_block(content)
                })

            # Detectar async/await
            if "async def" in content or "await " in content:
                patterns.append({
                    "name": "Async/Await Pattern",
                    "description": "Código assíncrono em Python",
                    "code": self._extract_code_block(content)
                })

            # Detectar WebSocket
            if "WebSocket" in content or "websocket" in content.lower():
                patterns.append({
                    "name": "WebSocket Pattern",
                    "description": "Implementação de WebSocket",
                    "code": self._extract_code_block(content)
                })

        # Remover duplicatas
        seen = set()
        unique_patterns = []
        for p in patterns:
            if p["name"] not in seen:
                seen.add(p["name"])
                unique_patterns.append(p)

        return unique_patterns[:3]  # Top 3

    def _extract_code_block(self, content: str) -> str:
        """Extrai primeiro bloco de código da mensagem."""
        import re

        match = re.search(r'```[\w]*\n(.*?)\n```', content, re.DOTALL)
        if match:
            return match.group(1)[:500]  # Limitar a 500 chars

        return ""


# Singleton
_chat_neo4j = None

def get_chat_neo4j() -> ChatNeo4jIntegration:
    """Retorna instância singleton da integração."""
    global _chat_neo4j
    if _chat_neo4j is None:
        _chat_neo4j = ChatNeo4jIntegration()
    return _chat_neo4j
