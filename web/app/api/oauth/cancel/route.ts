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
    const { OAuthService } = require('../../../../proto/budget/v1/oauth_connect');
    
    const transport = createGrpcWebTransport({ 
      baseUrl: process.env.NEXT_PUBLIC_GRPC_BASE_URL ?? "http://localhost:3030/grpc" 
    });
    
    const oauthClient = createClient(OAuthService, transport);
    
    console.log('Calling gRPC service for cancellation');
    
    // Вызываем gRPC метод
    await oauthClient.cancelAuth({
      authToken: token,
      telegramUserId: 'temp_user_id', // В реальной реализации это будет передаваться
    });

    return NextResponse.json({
      success: true,
      message: 'Authorization cancelled successfully',
    });
  } catch (error) {
    console.error('Error cancelling OAuth authorization:', error);
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
