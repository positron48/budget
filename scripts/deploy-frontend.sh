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
    
    # Проверка Node.js
    if ! command -v node &> /dev/null; then
        error "Node.js не установлен. Установите Node.js 20+"
        log "Для Ubuntu/Debian: curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash - && sudo apt-get install -y nodejs"
        exit 1
    fi
    
    local node_version=$(node --version | cut -d'v' -f2 | cut -d'.' -f1)
    if [ "$node_version" -lt 20 ]; then
        error "Требуется Node.js 20+, установлена версия: $(node --version)"
        exit 1
    fi
    
    success "Все зависимости установлены"
    log "Node.js версия: $(node --version)"
}

# Получение информации о релизе
get_release_info() {
    local tag="$1"
    
    if [ "$tag" = "latest" ] || [ "$tag" = "latest-build" ]; then
        # Для latest-build ищем draft release, для latest - последний опубликованный
        if [ "$tag" = "latest-build" ]; then
            log "Получение информации о последнем draft релизе (latest-build)..."
            local release_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/tags/latest-build"
        else
            log "Получение информации о последнем релизе..."
            local release_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
        fi
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
    chown -R budget:budget "$WEB_ROOT"
    chmod -R 755 "$WEB_ROOT"
    
    # Очистка временного файла
    rm -f "/tmp/$FRONTEND_ARCHIVE"
    
    success "Фронтенд установлен: $WEB_ROOT"
}

# Создание systemd сервиса для фронтенда
create_frontend_service() {
    local service_file="/etc/systemd/system/budget-frontend.service"
    
    # Определяем путь к node
    local node_path=$(which node)
    if [ -z "$node_path" ]; then
        error "Node.js не найден. Установите Node.js 20+"
        exit 1
    fi
    
    log "Создание systemd сервиса для фронтенда..."
    
    cat > "$service_file" << EOF
[Unit]
Description=Budget Frontend Service (Next.js Standalone)
After=network.target

[Service]
Type=simple
User=budget
Group=budget
WorkingDirectory=$WEB_ROOT
ExecStart=$node_path server.js
Restart=always
RestartSec=5
Environment=PORT=3000
Environment=NODE_ENV=production
Environment=HOSTNAME=0.0.0.0

# Логирование
StandardOutput=journal
StandardError=journal
SyslogIdentifier=budget-frontend

# Безопасность
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$WEB_ROOT

[Install]
WantedBy=multi-user.target
EOF
    
    systemctl daemon-reload
    success "Systemd сервис для фронтенда создан"
}

# Запуск сервиса фронтенда
start_frontend_service() {
    log "Запуск сервиса budget-frontend..."
    
    systemctl daemon-reload
    systemctl enable budget-frontend
    systemctl start budget-frontend
    
    # Проверка статуса
    sleep 3
    if systemctl is-active --quiet budget-frontend; then
        success "Сервис budget-frontend успешно запущен"
        log "Статус сервиса:"
        systemctl status budget-frontend --no-pager -l
    else
        error "Не удалось запустить сервис budget-frontend"
        log "Логи сервиса:"
        journalctl -u budget-frontend --no-pager -l -n 20
        exit 1
    fi
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
    create_frontend_service
    start_frontend_service
    
    success "Деплой фронтенда завершен успешно!"
    log "Версия: $release_tag"
    log "Путь: $WEB_ROOT"
    log "Сервис: budget-frontend"
}

# Запуск
main "$@"
