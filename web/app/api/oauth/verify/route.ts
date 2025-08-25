import { NextRequest, NextResponse } from 'next/server';

export async function POST(request: NextRequest) {
  try {
    const body = await request.json();
    const { token, verificationCode } = body;

    if (!token || !verificationCode) {
      return NextResponse.json(
        { error: 'Token and verification code are required' },
        { status: 400 }
      );
    }

    // Создаем gRPC клиент как в других местах фронтенда
    const { createClient } = require('@connectrpc/connect');
    const { createGrpcWebTransport } = require('@connectrpc/connect-web');
    const { OAuthService } = require('../../../../proto/budget/v1/oauth_pb');
    
    const transport = createGrpcWebTransport({ 
      baseUrl: process.env.ENVOY_URL 
    });
    
    const oauthClient = createClient(OAuthService, transport);
    
    console.log('Calling gRPC service for verification');
    
    // Вызываем gRPC метод
    const response = await oauthClient.verifyAuthCode({
      authToken: token,
      verificationCode: verificationCode,
      telegramUserId: 'temp_user_id', // В реальной реализации это будет передаваться
    });

    console.log('gRPC service response:', response);

    return NextResponse.json({
      success: true,
      message: 'Verification successful',
      tokens: response.tokens,
      sessionId: response.sessionId,
    });
  } catch (error: any) {
    console.error('Error during OAuth verification:', error);
    
    // Проверяем тип ошибки и возвращаем понятное сообщение
    if (error?.code === 3 && error?.rawMessage === 'invalid auth token') {
      return NextResponse.json(
        { error: 'Authorization token has expired or is invalid' },
        { status: 401 }
      );
    }
    
    if (error?.code === 3 && error?.rawMessage?.includes('invalid verification code')) {
      return NextResponse.json(
        { error: 'Invalid verification code' },
        { status: 400 }
      );
    }
    
    if (error?.code === 5 && error?.rawMessage?.includes('not found')) {
      return NextResponse.json(
        { error: 'Authorization token not found' },
        { status: 404 }
      );
    }
    
    // Для других ошибок возвращаем общее сообщение
    return NextResponse.json(
      { error: 'Failed to verify authorization code' },
      { status: 500 }
    );
  }
}
