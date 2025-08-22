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
    const { OAuthService } = require('../../../../proto/budget/v1/oauth_connect');
    
    const transport = createGrpcWebTransport({ 
      baseUrl: process.env.NEXT_PUBLIC_GRPC_BASE_URL ?? "http://localhost:3030/grpc" 
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
  } catch (error) {
    console.error('Error during OAuth verification:', error);
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
