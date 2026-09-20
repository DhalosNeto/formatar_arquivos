#!/usr/bin/env bash
# Falha quando a cobertura do domínio fica abaixo do mínimo do projeto.
set -euo pipefail

MINIMO=${MINIMO:-80}
PERFIL=${PERFIL:-coverage.out}

if [[ ! -f "$PERFIL" ]]; then
    echo "perfil de cobertura não encontrado: $PERFIL" >&2
    exit 1
fi

linhas_dominio=$(go tool cover -func="$PERFIL" | grep "/internal/domain/" || true)

if [[ -z "$linhas_dominio" ]]; then
    echo "nenhum pacote de domínio no perfil de cobertura — nada a verificar ainda"
    exit 0
fi

cobertura=$(awk '{ gsub("%","",$NF); soma += $NF; total += 1 } END { if (total > 0) printf "%.1f", soma/total; else print "0" }' <<< "$linhas_dominio")

echo "cobertura do domínio: ${cobertura}% (mínimo ${MINIMO}%)"

if (( $(echo "$cobertura < $MINIMO" | bc -l) )); then
    echo "cobertura do domínio abaixo do mínimo" >&2
    exit 1
fi
