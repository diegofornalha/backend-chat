#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Script de teste para validar configurações de encoding UTF-8
"""

import sys
import io
import json
from pathlib import Path

def test_encoding():
    """Testa se o encoding UTF-8 está funcionando corretamente"""

    print("=" * 60)
    print("🧪 Teste de Encoding UTF-8")
    print("=" * 60)
    print()

    # 1. Testar encoding do sistema
    print("1️⃣  Testando encoding do sistema...")
    print(f"   Encoding padrão: {sys.getdefaultencoding()}")
    print(f"   Encoding stdout: {sys.stdout.encoding}")
    print(f"   Encoding stderr: {sys.stderr.encoding}")
    print()

    # 2. Testar caracteres especiais
    print("2️⃣  Testando caracteres especiais...")
    test_strings = [
        "código",
        "função",
        "informação",
        "configuração",
        "integração",
        "português",
        "José da Silva",
        "São Paulo",
        "Ação, Reação, Solução",
        "Olá mundo! 👋",
        "Emoji: 🚀 💻 ⚡"
    ]

    for test_str in test_strings:
        try:
            print(f"   ✅ {test_str}")
        except UnicodeEncodeError as e:
            print(f"   ❌ Erro ao imprimir: {e}")
    print()

    # 3. Testar leitura/escrita de arquivo
    print("3️⃣  Testando leitura/escrita de arquivo...")
    test_file = Path("/tmp/test_encoding_utf8.txt")

    try:
        # Escrever arquivo
        with open(test_file, 'w', encoding='utf-8') as f:
            f.write("Este é um teste de código com acentuação!\n")
            f.write("Palavras: função, configuração, integração\n")
            f.write("Emoji: 🎉 🎊 ✨\n")

        # Ler arquivo
        with open(test_file, 'r', encoding='utf-8') as f:
            content = f.read()
            print(f"   ✅ Arquivo escrito e lido com sucesso")
            print(f"   Conteúdo: {content.strip()}")

        # Limpar
        test_file.unlink()

    except Exception as e:
        print(f"   ❌ Erro: {e}")
    print()

    # 4. Testar JSON
    print("4️⃣  Testando JSON com UTF-8...")
    test_data = {
        "mensagem": "Olá! Este é um teste de código",
        "função": "validação",
        "configuração": "UTF-8",
        "emojis": "🚀 💻 ⚡"
    }

    try:
        json_str = json.dumps(test_data, ensure_ascii=False, indent=2)
        print(f"   ✅ JSON serializado com sucesso:")
        print(f"   {json_str}")

        # Deserializar
        parsed = json.loads(json_str)
        print(f"   ✅ JSON deserializado: {parsed}")
    except Exception as e:
        print(f"   ❌ Erro: {e}")
    print()

    # 5. Resumo
    print("=" * 60)
    print("✅ Teste de encoding concluído!")
    print("=" * 60)
    print()
    print("Se todos os testes acima mostraram ✅, o encoding está")
    print("configurado corretamente e o problema de 'cÃ³digo' está resolvido!")
    print()

if __name__ == "__main__":
    test_encoding()
