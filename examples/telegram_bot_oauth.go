package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BotState представляет состояние бота для каждого пользователя
type BotState struct {
	State string // "idle", "waiting_email", "waiting_code"
	Email string
	Token string
}

// TelegramBot представляет Telegram бота с OAuth2 функциональностью
type TelegramBot struct {
	bot         *tgbotapi.BotAPI
	oauthClient budgetv1.OAuthServiceClient
	userStates  map[int64]*BotState // userID -> state
	webBaseURL  string
}

// NewTelegramBot создает новый экземпляр бота
func NewTelegramBot(token, grpcAddr, webBaseURL string) (*TelegramBot, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	// Подключение к gRPC сервису
	conn, err := grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	oauthClient := budgetv1.NewOAuthServiceClient(conn)

	return &TelegramBot{
		bot:         bot,
		oauthClient: oauthClient,
		userStates:  make(map[int64]*BotState),
		webBaseURL:  webBaseURL,
	}, nil
}

// Start запускает бота
func (b *TelegramBot) Start() {
	log.Printf("Bot started: @%s", b.bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		go b.handleMessage(update.Message)
	}
}

// handleMessage обрабатывает входящие сообщения
func (b *TelegramBot) handleMessage(message *tgbotapi.Message) {
	userID := message.From.ID
	text := message.Text

	// Получаем или создаем состояние пользователя
	state, exists := b.userStates[userID]
	if !exists {
		state = &BotState{State: "idle"}
		b.userStates[userID] = state
	}

	// Обработка команд
	if strings.HasPrefix(text, "/") {
		b.handleCommand(message, state)
		return
	}

	// Обработка текста в зависимости от состояния
	switch state.State {
	case "waiting_email":
		b.handleEmailInput(message, state)
	case "waiting_code":
		b.handleCodeInput(message, state)
	default:
		b.sendMessage(message.Chat.ID, "Используйте /login для авторизации или /help для справки.")
	}
}

// handleCommand обрабатывает команды
func (b *TelegramBot) handleCommand(message *tgbotapi.Message, state *BotState) {
	text := message.Text
	chatID := message.Chat.ID

	switch text {
	case "/start":
		b.sendMessage(chatID, "Добро пожаловать в Budget Bot! Используйте /login для авторизации.")
		state.State = "idle"

	case "/login":
		if state.State != "idle" {
			b.sendMessage(chatID, "Завершите текущий процесс авторизации или используйте /cancel для отмены.")
			return
		}
		b.startLogin(chatID, state)

	case "/cancel":
		b.cancelCurrentProcess(chatID, state)

	case "/logout":
		b.logout(chatID, state)

	case "/help":
		b.showHelp(chatID)

	default:
		b.sendMessage(chatID, "Неизвестная команда. Используйте /help для справки.")
	}
}

// startLogin начинает процесс авторизации
func (b *TelegramBot) startLogin(chatID int64, state *BotState) {
	state.State = "waiting_email"
	state.Email = ""
	state.Token = ""

	msg := tgbotapi.NewMessage(chatID,
		"Для авторизации введите ваш email адрес:\n\n"+
			"Этот email должен совпадать с email в вашем аккаунте Budget.")

	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "cancel"),
		),
	)

	b.bot.Send(msg)
}

// handleEmailInput обрабатывает ввод email
func (b *TelegramBot) handleEmailInput(message *tgbotapi.Message, state *BotState) {
	email := strings.TrimSpace(message.Text)
	chatID := message.Chat.ID

	// Простая валидация email
	if !strings.Contains(email, "@") {
		b.sendMessage(chatID, "Пожалуйста, введите корректный email адрес.")
		return
	}

	state.Email = email

	// Генерация ссылки авторизации
	authURL, authToken, expiresAt, err := b.generateAuthLink(email, message.From.ID)
	if err != nil {
		b.sendMessage(chatID, "Ошибка при создании ссылки авторизации. Попробуйте позже.")
		log.Printf("Failed to generate auth link: %v", err)
		return
	}

	state.Token = authToken
	state.State = "waiting_code"

	// Создаем inline кнопку для открытия ссылки
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Открыть в браузере", authURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Отмена", "cancel"),
		),
	)

	expiresIn := time.Until(expiresAt.AsTime())
	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("Ссылка для авторизации создана!\n\n"+
			"1. Нажмите кнопку 'Открыть в браузере'\n"+
			"2. Введите логин и пароль от вашего аккаунта\n"+
			"3. Скопируйте код подтверждения\n"+
			"4. Введите код в этом чате\n\n"+
			"⏰ Ссылка действительна %d минут",
			int(expiresIn.Minutes())))

	msg.ReplyMarkup = keyboard
	b.bot.Send(msg)
}

// handleCodeInput обрабатывает ввод кода подтверждения
func (b *TelegramBot) handleCodeInput(message *tgbotapi.Message, state *BotState) {
	code := strings.TrimSpace(message.Text)
	chatID := message.Chat.ID

	// Проверяем, что код состоит из 6 цифр
	if len(code) != 6 {
		b.sendMessage(chatID, "Код подтверждения должен состоять из 6 цифр. Попробуйте еще раз.")
		return
	}

	// Верификация кода
	_, sessionID, err := b.verifyAuthCode(state.Token, code, message.From.ID)
	if err != nil {
		b.sendMessage(chatID, "Неверный код подтверждения. Попробуйте еще раз.")
		log.Printf("Failed to verify auth code: %v", err)
		return
	}

	// Успешная авторизация
	state.State = "idle"
	state.Email = ""
	state.Token = ""

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ Авторизация успешна!\n\n"+
			"Добро пожаловать, %s!\n"+
			"Теперь вы можете использовать все функции бота.\n\n"+
			"Используйте /help для просмотра доступных команд.",
			message.From.FirstName))

	b.bot.Send(msg)

	// Сохраняем информацию о сессии (в реальном приложении)
	log.Printf("User %d authorized successfully, session ID: %s", message.From.ID, sessionID)
}

// generateAuthLink генерирует ссылку авторизации
func (b *TelegramBot) generateAuthLink(email string, userID int64) (string, string, *timestamppb.Timestamp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &budgetv1.GenerateAuthLinkRequest{
		Email:          email,
		TelegramUserId: strconv.FormatInt(userID, 10),
		UserAgent:      "TelegramBot/1.0",
		IpAddress:      "127.0.0.1", // В реальном приложении получать из контекста
	}

	resp, err := b.oauthClient.GenerateAuthLink(ctx, req)
	if err != nil {
		return "", "", nil, err
	}

	return resp.AuthUrl, resp.AuthToken, resp.ExpiresAt, nil
}

// verifyAuthCode верифицирует код подтверждения
func (b *TelegramBot) verifyAuthCode(authToken, verificationCode string, userID int64) (*budgetv1.TokenPair, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &budgetv1.VerifyAuthCodeRequest{
		AuthToken:        authToken,
		VerificationCode: verificationCode,
		TelegramUserId:   strconv.FormatInt(userID, 10),
	}

	resp, err := b.oauthClient.VerifyAuthCode(ctx, req)
	if err != nil {
		return nil, "", err
	}

	return resp.Tokens, resp.SessionId, nil
}

// cancelCurrentProcess отменяет текущий процесс
func (b *TelegramBot) cancelCurrentProcess(chatID int64, state *BotState) {
	if state.State == "idle" {
		b.sendMessage(chatID, "Нет активного процесса для отмены.")
		return
	}

	// Отменяем авторизацию на сервере
	if state.Token != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req := &budgetv1.CancelAuthRequest{
			AuthToken:      state.Token,
			TelegramUserId: "temp_user_id", // В реальном приложении передавать userID
		}

		b.oauthClient.CancelAuth(ctx, req)
	}

	// Сбрасываем состояние
	state.State = "idle"
	state.Email = ""
	state.Token = ""

	b.sendMessage(chatID, "Процесс отменен. Используйте /login для новой попытки авторизации.")
}

// logout выходит из системы
func (b *TelegramBot) logout(chatID int64, state *BotState) {
	// TODO: Реализовать отзыв всех сессий пользователя
	state.State = "idle"
	state.Email = ""
	state.Token = ""

	b.sendMessage(chatID, "Вы вышли из системы. Используйте /login для повторной авторизации.")
}

// showHelp показывает справку
func (b *TelegramBot) showHelp(chatID int64) {
	helpText := `📋 Доступные команды:

/login - Авторизация в системе
/logout - Выход из системы
/cancel - Отмена текущего процесса
/help - Показать эту справку

🔐 Процесс авторизации:
1. Введите команду /login
2. Укажите ваш email
3. Перейдите по ссылке в браузере
4. Введите логин и пароль
5. Скопируйте код подтверждения
6. Введите код в боте

❓ Если у вас возникли проблемы, попробуйте /cancel и начните заново.`

	b.sendMessage(chatID, helpText)
}

// sendMessage отправляет сообщение
func (b *TelegramBot) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	b.bot.Send(msg)
}

func main() {
	// Конфигурация
	botToken := "YOUR_BOT_TOKEN"
	grpcAddr := "localhost:8080"
	webBaseURL := "http://localhost:3000"

	// Создание бота
	bot, err := NewTelegramBot(botToken, grpcAddr, webBaseURL)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	// Запуск бота
	bot.Start()
}
