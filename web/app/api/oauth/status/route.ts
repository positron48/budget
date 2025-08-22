import { NextRequest, NextResponse } from 'next/server';

export async function GET(request: NextRequest) {
  try {
    const { searchParams } = new URL(request.url);
    const token = searchParams.get('token');

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
    
    console.log('Calling gRPC service for auth status with token:', token);
    
    // Вызываем gRPC метод
    const response = await oauthClient.getAuthStatus({ authToken: token });
    
    console.log('gRPC service response:', response);
    
    return NextResponse.json({
      status: response.status,
      createdAt: response.createdAt,
      expiresAt: response.expiresAt,
      email: response.email,
    });
  } catch (error) {
    console.error('Error checking OAuth status:', error);
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
