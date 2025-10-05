#!/bin/bash

# Script para iniciar Backend Go com Anthropic SDK

echo "🚀 Iniciando Backend Go..."

# Verificar se ANTHROPIC_API_KEY está configurada
if [ -z "$ANTHROPIC_API_KEY" ]; then
    echo "❌ ANTHROPIC_API_KEY não configurada"
    echo ""
    echo "Configure com:"
    echo "export ANTHROPIC_API_KEY='sua-api-key-aqui'"
    exit 1
fi

# Compilar e executar
echo "📦 Compilando Go..."
go build -o server main.go

echo "✅ Iniciando servidor na porta 8000..."
./server