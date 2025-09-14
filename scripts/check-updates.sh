#!/bin/bash

# Скрипт для проверки доступных обновлений
# Использование: ./scripts/check-updates.sh

set -e

# Конфигурация
REPO_OWNER="positron48"
REPO_NAME="budget"
CURRENT_VERSION_FILE="/var/www/budget/version"

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

# Получение текущей версии
get_current_version() {
    if [ -f "$CURRENT_VERSION_FILE" ]; then
        cat "$CURRENT_VERSION_FILE"
    else
        echo "unknown"
    fi
}

# Получение последней версии
get_latest_version() {
    local release_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
    local release_info=$(curl -s "$release_url")
    
    if echo "$release_info" | jq -e '.message' | grep -q "Not Found"; then
        error "Не удалось получить информацию о релизах"
        exit 1
    fi
    
    echo "$release_info" | jq -r '.tag_name'
}

# Получение информации о релизе
get_release_info() {
    local tag="$1"
    local release_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/tags/${tag}"
    local release_info=$(curl -s "$release_url")
    
    if echo "$release_info" | jq -e '.message' | grep -q "Not Found"; then
        error "Релиз $tag не найден"
        exit 1
    fi
    
    echo "$release_info"
}

# Проверка доступности артефактов
check_artifacts() {
    local release_info="$1"
    local arch="$2"
    
    log "Проверка доступности артефактов..."
    
    # Проверка бэкенда
    local backend_artifact="budgetd-${arch}"
    local backend_url=$(echo "$release_info" | jq -r ".assets[] | select(.name == \"$backend_artifact\") | .browser_download_url")
    
    if [ -z "$backend_url" ] || [ "$backend_url" = "null" ]; then
        warning "Артефакт бэкенда $backend_artifact не найден"
        return 1
    fi
    
    # Проверка фронтенда
    local frontend_artifact="frontend-build.tar.gz"
    local frontend_url=$(echo "$release_info" | jq -r ".assets[] | select(.name == \"$frontend_artifact\") | .browser_download_url")
    
    if [ -z "$frontend_url" ] || [ "$frontend_url" = "null" ]; then
        warning "Артефакт фронтенда $frontend_artifact не найден"
        return 1
    fi
    
    success "Все артефакты доступны"
    return 0
}

# Определение архитектуры
detect_arch() {
    case "$(uname -m)" in
        x86_64)
            if [ "$(uname -s)" = "Linux" ]; then
                echo "linux-amd64"
            elif [ "$(uname -s)" = "Darwin" ]; then
                echo "darwin-amd64"
            else
                echo "linux-amd64"
            fi
            ;;
        aarch64|arm64)
            if [ "$(uname -s)" = "Linux" ]; then
                echo "linux-arm64"
            elif [ "$(uname -s)" = "Darwin" ]; then
                echo "darwin-arm64"
            else
                echo "linux-arm64"
            fi
            ;;
        *)
            echo "unknown"
            ;;
    esac
}

# Основная функция
main() {
    log "Проверка доступных обновлений..."
    
    # Проверка зависимостей
    if ! command -v curl &> /dev/null; then
        error "curl не установлен"
        exit 1
    fi
    
    if ! command -v jq &> /dev/null; then
        error "jq не установлен"
        exit 1
    fi
    
    local current_version=$(get_current_version)
    local latest_version=$(get_latest_version)
    local arch=$(detect_arch)
    
    log "Текущая версия: $current_version"
    log "Последняя версия: $latest_version"
    log "Архитектура: $arch"
    
    if [ "$current_version" = "$latest_version" ]; then
        success "У вас установлена последняя версия: $latest_version"
        exit 0
    fi
    
    if [ "$current_version" = "unknown" ]; then
        warning "Не удалось определить текущую версию"
        log "Доступна версия: $latest_version"
    else
        log "Доступно обновление: $current_version -> $latest_version"
    fi
    
    # Проверка артефактов
    local release_info=$(get_release_info "$latest_version")
    if check_artifacts "$release_info" "$arch"; then
        success "Обновление готово к установке"
        log "Для обновления выполните:"
        log "  sudo ./scripts/deploy-all.sh $latest_version"
    else
        warning "Не все артефакты доступны для обновления"
    fi
}

# Запуск
main "$@"
