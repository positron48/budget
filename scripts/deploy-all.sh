#!/bin/bash

# Скрипт для полного деплоя (бэкенд + фронтенд)
# Использование: ./scripts/deploy-all.sh [tag] [arch]
# tag - тег релиза (например, v1.0.0) или latest для последнего артефакта
# arch - архитектура для бэкенда (linux-amd64, linux-arm64, windows-amd64, darwin-amd64, darwin-arm64)

set -e

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Функция для логирования
log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Получение пути к скриптам
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Основная функция
main() {
    local tag="${1:-latest}"
    local arch="${2:-}"
    
    log "Начало полного деплоя..."
    log "Тег: $tag"
    log "Архитектура: ${arch:-автоопределение}"
    
    # Проверка прав
    if [ "$EUID" -ne 0 ]; then
        error "Скрипт должен быть запущен с правами root"
        exit 1
    fi
    
    # Деплой бэкенда
    log "=== ДЕПЛОЙ БЭКЕНДА ==="
    if ! "$SCRIPT_DIR/deploy-backend.sh" "$tag" "$arch"; then
        error "Ошибка при деплое бэкенда"
        exit 1
    fi
    
    # Небольшая пауза между деплоями
    sleep 5
    
    # Деплой фронтенда
    log "=== ДЕПЛОЙ ФРОНТЕНДА ==="
    if ! "$SCRIPT_DIR/deploy-frontend.sh" "$tag"; then
        error "Ошибка при деплое фронтенда"
        exit 1
    fi
    
    success "Полный деплой завершен успешно!"
    log "Бэкенд: systemctl status budget-backend"
    log "Фронтенд: http://localhost"
    log "Логи бэкенда: journalctl -u budget-backend -f"
    log "Логи nginx: journalctl -u nginx -f"
}

# Запуск
main "$@"
