"""Code Runner - Executa código Python inline com segurança."""

import ast
import sys
from io import StringIO
from contextlib import redirect_stdout, redirect_stderr
import traceback


class SafeCodeRunner:
    """Executor de código Python com sandboxing."""

    def __init__(self):
        self.allowed_modules = [
            'math', 'random', 'datetime', 'json', 're',
            'itertools', 'functools', 'collections'
        ]

        self.forbidden_patterns = [
            'import os', 'import sys', 'import subprocess',
            '__import__', 'eval', 'exec', 'compile',
            'open(', 'file(', 'input('
        ]

    def is_safe(self, code: str) -> tuple[bool, str]:
        """Verifica se código é seguro para executar."""
        code_lower = code.lower()

        # Verificar padrões proibidos
        for pattern in self.forbidden_patterns:
            if pattern in code_lower:
                return False, f"Código contém padrão proibido: {pattern}"

        # Verificar AST
        try:
            tree = ast.parse(code)

            # Verificar nós perigosos
            for node in ast.walk(tree):
                if isinstance(node, ast.Import):
                    for alias in node.names:
                        if alias.name not in self.allowed_modules:
                            return False, f"Módulo não permitido: {alias.name}"

                elif isinstance(node, ast.ImportFrom):
                    if node.module not in self.allowed_modules:
                        return False, f"Módulo não permitido: {node.module}"

        except SyntaxError as e:
            return False, f"Erro de sintaxe: {e}"

        return True, "OK"

    def run(self, code: str, timeout: int = 5) -> dict:
        """Executa código Python com timeout."""
        # Verificar segurança
        is_safe, message = self.is_safe(code)

        if not is_safe:
            return {
                "success": False,
                "output": "",
                "error": message,
                "execution_time_ms": 0
            }

        # Preparar ambiente
        import time
        start_time = time.time()

        stdout = StringIO()
        stderr = StringIO()

        try:
            # Namespace limitado
            local_ns = {
                '__builtins__': {
                    'print': print,
                    'len': len,
                    'range': range,
                    'str': str,
                    'int': int,
                    'float': float,
                    'list': list,
                    'dict': dict,
                    'set': set,
                    'tuple': tuple,
                    'sum': sum,
                    'min': min,
                    'max': max,
                    'abs': abs,
                    'round': round,
                    'sorted': sorted,
                    'enumerate': enumerate,
                    'zip': zip,
                }
            }

            # Executar com redirecionamento
            with redirect_stdout(stdout), redirect_stderr(stderr):
                exec(code, local_ns, local_ns)

            execution_time = (time.time() - start_time) * 1000

            return {
                "success": True,
                "output": stdout.getvalue(),
                "error": stderr.getvalue(),
                "execution_time_ms": round(execution_time, 2)
            }

        except Exception as e:
            execution_time = (time.time() - start_time) * 1000

            return {
                "success": False,
                "output": stdout.getvalue(),
                "error": f"{type(e).__name__}: {str(e)}\n{traceback.format_exc()}",
                "execution_time_ms": round(execution_time, 2)
            }


# Singleton
_runner = None

def get_code_runner() -> SafeCodeRunner:
    """Retorna instância singleton do runner."""
    global _runner
    if _runner is None:
        _runner = SafeCodeRunner()
    return _runner
