#!/bin/bash

# Скрипт для деплоя фронтенда с использованием артефактов GitHub
# Использование: ./scripts/deploy-frontend.sh [tag]
# tag - тег релиза (например, v1.0.0) или latest для последнего артефакта

set -e

# Конфигурация
REPO_OWNER="positron48"
REPO_NAME="budget"
FRONTEND_ARCHIVE="frontend-build.tar.gz"
WEB_ROOT="/var/www/budget/web"

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

# Проверка зависимостей
check_dependencies() {
    log "Проверка зависимостей..."
    
    if ! command -v curl &> /dev/null; then
        error "curl не установлен"
        exit 1
    fi
    
    if ! command -v jq &> /dev/null; then
        error "jq не установлен"
        exit 1
    fi
    
    success "Все зависимости установлены"
}

# Получение информации о релизе
get_release_info() {
    local tag="$1"
    
    if [ "$tag" = "latest" ]; then
        log "Получение информации о последнем релизе..."
        local release_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
    else
        log "Получение информации о релизе $tag..."
        local release_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/tags/${tag}"
    fi
    
    local release_info=$(curl -s "$release_url")
    
    if echo "$release_info" | jq -e '.message' | grep -q "Not Found"; then
        error "Релиз $tag не найден"
        exit 1
    fi
    
    echo "$release_info"
}

# Скачивание фронтенда
download_frontend() {
    local release_info="$1"
    
    log "Поиск артефакта $FRONTEND_ARCHIVE..."
    
    local download_url=$(echo "$release_info" | jq -r ".assets[] | select(.name == \"$FRONTEND_ARCHIVE\") | .browser_download_url")
    
    if [ -z "$download_url" ] || [ "$download_url" = "null" ]; then
        error "Артефакт $FRONTEND_ARCHIVE не найден в релизе"
        exit 1
    fi
    
    log "Скачивание $FRONTEND_ARCHIVE..."
    curl -L -o "/tmp/$FRONTEND_ARCHIVE" "$download_url"
    
    if [ ! -f "/tmp/$FRONTEND_ARCHIVE" ]; then
        error "Не удалось скачать фронтенд"
        exit 1
    fi
    
    success "Фронтенд скачан: /tmp/$FRONTEND_ARCHIVE"
}


# Установка фронтенда
install_frontend() {
    log "Установка фронтенда..."
    
    # Создание директории если не существует
    mkdir -p "$WEB_ROOT"
    
    # Очистка текущего содержимого
    rm -rf "$WEB_ROOT"/*
    
    # Распаковка нового фронтенда
    tar -xzf "/tmp/$FRONTEND_ARCHIVE" -C "$WEB_ROOT"
    
    # Установка прав
    chown -R www-data:www-data "$WEB_ROOT"
    chmod -R 755 "$WEB_ROOT"
    
    # Очистка временного файла
    rm -f "/tmp/$FRONTEND_ARCHIVE"
    
    success "Фронтенд установлен: $WEB_ROOT"
}


# Основная функция
main() {
    local tag="${1:-latest}"
    
    log "Начало деплоя фронтенда..."
    log "Тег: $tag"
    
    # Проверка прав
    if [ "$EUID" -ne 0 ]; then
        error "Скрипт должен быть запущен с правами root"
        exit 1
    fi
    
    check_dependencies
    
    local release_info=$(get_release_info "$tag")
    local release_tag=$(echo "$release_info" | jq -r '.tag_name')
    log "Релиз: $release_tag"
    
    download_frontend "$release_info"
    install_frontend
    
    success "Деплой фронтенда завершен успешно!"
    log "Версия: $release_tag"
    log "Путь: $WEB_ROOT"
}

# Запуск
main "$@"
