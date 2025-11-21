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

# Проверка и запуск Docker сервисов (БД и Redis)
ensure_docker_services() {
    log "Проверка Docker сервисов (БД и Redis)..."
    
    if ! command -v docker &> /dev/null; then
        error "Docker не установлен"
        exit 1
    fi
    
    if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
        error "Docker Compose не установлен"
        exit 1
    fi
    
    # Переход в директорию проекта
    cd "$PROJECT_DIR" || exit 1
    
    # Запуск БД, Redis и Envoy через docker-compose.prod.yml
    if [ -f "docker-compose.prod.yml" ]; then
        log "Запуск БД, Redis и Envoy через docker-compose.prod.yml..."
        docker compose -f docker-compose.prod.yml up -d
        success "Docker сервисы запущены"
    else
        warning "docker-compose.prod.yml не найден, используем docker-compose.yml"
        docker compose up -d db redis envoy
        success "Docker сервисы запущены"
    fi
    
    # Небольшая пауза для инициализации
    sleep 3
}

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
    
    # Запуск Docker сервисов (БД и Redis)
    ensure_docker_services
    
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
    log "Бэкенд: systemctl status budgetd"
    log "Фронтенд: systemctl status budget-frontend"
    log "Логи бэкенда: journalctl -u budgetd -f"
    log "Логи фронтенда: journalctl -u budget-frontend -f"
    log "Docker сервисы: docker compose -f docker-compose.prod.yml ps"
}

# Запуск
main "$@"
