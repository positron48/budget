#!/bin/bash

# Скрипт для деплоя бэкенда с использованием артефактов GitHub
# Использование: ./scripts/deploy-backend.sh [tag] [arch]
# tag - тег релиза (например, v1.0.0) или latest для последнего артефакта
# arch - архитектура (linux-amd64, linux-arm64, windows-amd64, darwin-amd64, darwin-arm64)

set -e

# Конфигурация
REPO_OWNER="positron48"
REPO_NAME="budget"
BINARY_NAME="budgetd"
SERVICE_NAME="budgetd"
INSTALL_DIR="/var/www/budget/bin"
SYSTEMD_SERVICE="/etc/systemd/system/budgetd.service"

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
    
    if ! command -v systemctl &> /dev/null; then
        error "systemctl не найден (требуется systemd)"
        exit 1
    fi
    
    success "Все зависимости установлены"
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

# Определение архитектуры
detect_arch() {
    local arch="$1"
    
    if [ -z "$arch" ]; then
        case "$(uname -m)" in
            x86_64)
                if [ "$(uname -s)" = "Linux" ]; then
                    arch="linux-amd64"
                elif [ "$(uname -s)" = "Darwin" ]; then
                    arch="darwin-amd64"
                else
                    arch="linux-amd64"
                fi
                ;;
            aarch64|arm64)
                if [ "$(uname -s)" = "Linux" ]; then
                    arch="linux-arm64"
                elif [ "$(uname -s)" = "Darwin" ]; then
                    arch="darwin-arm64"
                else
                    arch="linux-arm64"
                fi
                ;;
            *)
                error "Неподдерживаемая архитектура: $(uname -m)"
                exit 1
                ;;
        esac
    fi
    
    echo "$arch"
}

# Скачивание бинарника
download_binary() {
    local release_info="$1"
    local arch="$2"
    local binary_name="${BINARY_NAME}-${arch}"
    
    log "Поиск артефакта $binary_name..."
    
    local download_url=$(echo "$release_info" | jq -r ".assets[] | select(.name == \"$binary_name\") | .browser_download_url")
    
    if [ -z "$download_url" ] || [ "$download_url" = "null" ]; then
        error "Артефакт $binary_name не найден в релизе"
        exit 1
    fi
    
    log "Скачивание $binary_name..."
    curl -L -o "/tmp/$binary_name" "$download_url"
    
    if [ ! -f "/tmp/$binary_name" ]; then
        error "Не удалось скачать бинарник"
        exit 1
    fi
    
    chmod +x "/tmp/$binary_name"
    success "Бинарник скачан: /tmp/$binary_name"
}


# Установка бинарника
install_binary() {
    local arch="$1"
    local binary_name="${BINARY_NAME}-${arch}"
    
    log "Установка бинарника..."
    
    # Создание директории если не существует
    mkdir -p "$INSTALL_DIR"
    
    # Остановка сервиса
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        log "Остановка сервиса $SERVICE_NAME..."
        systemctl stop "$SERVICE_NAME"
    fi
    
    # Установка нового бинарника
    cp "/tmp/$binary_name" "$INSTALL_DIR/$BINARY_NAME"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    
    # Очистка временного файла
    rm -f "/tmp/$binary_name"
    
    success "Бинарник установлен: $INSTALL_DIR/$BINARY_NAME"
}

# Создание systemd сервиса
create_systemd_service() {
    if [ ! -f "$SYSTEMD_SERVICE" ]; then
        log "Создание systemd сервиса..."
        
        cat > "$SYSTEMD_SERVICE" << EOF
[Unit]
Description=Budget Backend Service
After=network.target postgresql.service redis.service
Wants=postgresql.service redis.service

[Service]
Type=simple
User=budget
Group=budget
WorkingDirectory=/var/www/budget
ExecStart=$INSTALL_DIR/$BINARY_NAME
Restart=always
RestartSec=5
Environment=PORT=8080
Environment=GRPC_ADDR=0.0.0.0:8080
EnvironmentFile=-/var/www/budget/.env

# Логирование
StandardOutput=journal
StandardError=journal
SyslogIdentifier=budget-backend

# Безопасность
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/www/budget

[Install]
WantedBy=multi-user.target
EOF
        
        systemctl daemon-reload
        success "Systemd сервис создан"
    fi
}

# Создание пользователя
create_user() {
    if ! id "budget" &>/dev/null; then
        log "Создание пользователя budget..."
        useradd -r -s /bin/false -d /var/www/budget budget
        mkdir -p /var/www/budget
        chown budget:budget /var/www/budget
        success "Пользователь budget создан"
    fi
}

# Запуск сервиса
start_service() {
    log "Запуск сервиса $SERVICE_NAME..."
    
    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"
    systemctl start "$SERVICE_NAME"
    
    # Проверка статуса
    sleep 3
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        success "Сервис $SERVICE_NAME успешно запущен"
        log "Статус сервиса:"
        systemctl status "$SERVICE_NAME" --no-pager -l
    else
        error "Не удалось запустить сервис $SERVICE_NAME"
        log "Логи сервиса:"
        journalctl -u "$SERVICE_NAME" --no-pager -l -n 20
        exit 1
    fi
}

# Основная функция
main() {
    local tag="${1:-latest}"
    local arch="${2:-}"
    
    log "Начало деплоя бэкенда..."
    log "Тег: $tag"
    
    # Проверка прав
    if [ "$EUID" -ne 0 ]; then
        error "Скрипт должен быть запущен с правами root"
        exit 1
    fi
    
    check_dependencies
    arch=$(detect_arch "$arch")
    log "Архитектура: $arch"
    
    local release_info=$(get_release_info "$tag")
    local release_tag=$(echo "$release_info" | jq -r '.tag_name')
    log "Релиз: $release_tag"
    
    download_binary "$release_info" "$arch"
    create_user
    install_binary "$arch"
    create_systemd_service
    start_service
    
    success "Деплой бэкенда завершен успешно!"
    log "Версия: $release_tag"
    log "Бинарник: $INSTALL_DIR/$BINARY_NAME"
    log "Сервис: $SERVICE_NAME"
}

# Запуск
main "$@"
