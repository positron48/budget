import { NextRequest, NextResponse } from 'next/server';

export async function POST(request: NextRequest) {
  try {
    const body = await request.json();
    const { token } = body;

    if (!token) {
      return NextResponse.json(
        { error: 'Token is required' },
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
    
    console.log('Calling gRPC service for verification code');
    
    // Вызываем gRPC метод
    const response = await oauthClient.getVerificationCode({
      authToken: token,
    });

    console.log('gRPC service response:', response);

    return NextResponse.json({
      verificationCode: response.verificationCode,
      message: response.message || 'Verification code generated successfully',
    });
  } catch (error: any) {
    console.error('Error during OAuth login:', error);
    
    // Проверяем тип ошибки и возвращаем понятное сообщение
    if (error?.code === 3 && error?.rawMessage === 'invalid auth token') {
      return NextResponse.json(
        { error: 'Authorization token has expired or is invalid' },
        { status: 401 }
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
      { error: 'Failed to generate verification code' },
      { status: 500 }
    );
  }
}
