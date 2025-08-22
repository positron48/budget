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

    // TODO: Реализовать вызов gRPC сервиса для проверки статуса
    // Пока возвращаем заглушку
    const response = await fetch(`${process.env.GRPC_GATEWAY_URL}/v1/oauth/status`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ auth_token: token }),
    });

    if (!response.ok) {
      throw new Error('Failed to check token status');
    }

    const data = await response.json();
    
    return NextResponse.json({
      status: data.status,
      createdAt: data.created_at,
      expiresAt: data.expires_at,
      email: data.email,
    });
  } catch (error) {
    console.error('Error checking OAuth status:', error);
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
