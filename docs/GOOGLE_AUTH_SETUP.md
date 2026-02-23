# Google Auth Setup (Web)

Инструкция для входа/регистрации через Google в `budget`.

## 1) Что создать в Google Cloud

1. Откройте [Google Cloud Console](https://console.cloud.google.com/).
2. Создайте (или выберите) проект.
3. Откройте `APIs & Services -> OAuth consent screen`.
4. Заполните OAuth consent screen:
   - App name
   - Support email
   - Authorized domains (для прода: ваш домен, например `qantrix.ru`)
5. Откройте `APIs & Services -> Credentials`.
6. Нажмите `Create Credentials -> OAuth client ID`.
7. Тип: `Web application`.
8. Добавьте `Authorized JavaScript origins`:
   - Локально: `http://localhost:3030`
   - Прод: `https://budget.qantrix.ru`
9. Добавьте `Authorized redirect URIs` (можно оставить пустым для GIS popup, но обычно добавляют):
   - `https://budget.qantrix.ru`
   - `http://localhost:3030`
10. Сохраните и скопируйте `Client ID` (формат `xxxxx.apps.googleusercontent.com`).

## 2) Куда прописать в `.env`

Файл: `budget/env.example` (или ваш реальный `.env` рядом с `docker-compose`).

Нужны 2 переменные:

```bash
GOOGLE_CLIENT_ID=xxxxx.apps.googleusercontent.com
NEXT_PUBLIC_GOOGLE_CLIENT_ID=xxxxx.apps.googleusercontent.com
AUTH_PASSWORD_ENABLED=false
```

- `GOOGLE_CLIENT_ID` использует backend для проверки `id_token` (audience check).
- `NEXT_PUBLIC_GOOGLE_CLIENT_ID` использует web-клиент Google Identity Services.
- `AUTH_PASSWORD_ENABLED=false` отключает legacy login/register по email+password в backend API.

## 3) Локальная проверка

1. Заполните переменные в локальном `.env`.
2. Перезапустите сервисы:

```bash
cd budget
make restart
```

3. Откройте `http://localhost:3030/login` или `http://localhost:3030/register`.
4. Нажмите `Continue with Google`.

## 4) k3s / Flux (devops-time-host)

Переменные уже заведены в `ConfigMap`:
- `devops-time-host/apps/budget/base/configmap.yaml`
  - `GOOGLE_CLIENT_ID`
  - `NEXT_PUBLIC_GOOGLE_CLIENT_ID`

И прокинуты в deployment:
- `devops-time-host/apps/budget/base/app-deployment.yaml` (`GOOGLE_CLIENT_ID`)
- `devops-time-host/apps/budget/base/web-deployment.yaml` (`NEXT_PUBLIC_GOOGLE_CLIENT_ID`)

Заполните значения и запушьте изменения в `devops-time-host`, Flux подхватит.

## 5) Как создать/обновить secret в k3s

В `budget` уже используется secret `budget-secrets` для JWT.
Google ID можно хранить в ConfigMap (это не секрет), но если хотите хранить в Secret — используйте команду ниже.

### Вариант A (рекомендуется для текущего манифеста): через ConfigMap

Просто заполните `GOOGLE_CLIENT_ID` и `NEXT_PUBLIC_GOOGLE_CLIENT_ID` в `configmap.yaml`.

### Вариант B (через Secret, вручную на сервере)

```bash
kubectl create namespace budget --dry-run=client -o yaml | kubectl apply -f -

kubectl -n budget create secret generic budget-secrets \
  --from-literal=JWT_SIGN_KEY='REPLACE_WITH_STRONG_SECRET' \
  --from-literal=JWT_ACCESS_TTL='15m' \
  --from-literal=JWT_REFRESH_TTL='720h' \
  --from-literal=GOOGLE_CLIENT_ID='xxxxx.apps.googleusercontent.com' \
  --dry-run=client -o yaml | kubectl apply -f -
```

Если выберете вариант B, нужно переключить `app-deployment.yaml` на `secretKeyRef` для `GOOGLE_CLIENT_ID`.

## 6) Типовые проблемы

- `google auth is not configured`:
  - не задан `NEXT_PUBLIC_GOOGLE_CLIENT_ID` на фронте или `GOOGLE_CLIENT_ID` на бэке.
- `google token audience mismatch`:
  - `GOOGLE_CLIENT_ID` на бэке не совпадает с client id, которым подписан токен.
- popup не открывается:
  - origin не добавлен в `Authorized JavaScript origins`.
