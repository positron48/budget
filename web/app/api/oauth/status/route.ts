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
    const { OAuthService } = require('../../../../proto/budget/v1/oauth_pb');
    
    const transport = createGrpcWebTransport({ 
      baseUrl: process.env.ENVOY_URL 
    });
    
    const oauthClient = createClient(OAuthService, transport);
    
    console.log('Calling gRPC service for auth status with token:', token);
    
    // Вызываем gRPC метод
    const response = await oauthClient.getAuthStatus({ authToken: token });
    
    console.log('gRPC service response:', response);
    
    // Конвертируем timestamps в ISO строки (protobuf timestamps содержат BigInt)
    // Connect RPC может возвращать timestamps как Date или как объект с seconds/nanos
    const convertTimestamp = (ts: any): string | null => {
      if (!ts) return null;
      if (ts instanceof Date) return ts.toISOString();
      if (typeof ts === 'string') return ts;
      if (ts.seconds !== undefined) {
        // Protobuf timestamp: { seconds: BigInt, nanos: number }
        const seconds = typeof ts.seconds === 'bigint' ? Number(ts.seconds) : ts.seconds;
        const nanos = ts.nanos || 0;
        return new Date(seconds * 1000 + nanos / 1000000).toISOString();
      }
      return null;
    };
    
    const createdAt = convertTimestamp(response.createdAt);
    const expiresAt = convertTimestamp(response.expiresAt);
    
    return NextResponse.json({
      status: response.status,
      createdAt,
      expiresAt,
      email: response.email,
    });
  } catch (error: any) {
    console.error('Error checking OAuth status:', error);
    
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
      { error: 'Failed to check authorization status' },
      { status: 500 }
    );
  }
}
