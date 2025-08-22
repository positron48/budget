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

    // TODO: Реализовать вызов gRPC сервиса для верификации
    // Пока возвращаем заглушку
    const response = await fetch(`${process.env.GRPC_GATEWAY_URL}/v1/oauth/verify`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        auth_token: token,
        verification_code: verificationCode,
        telegram_user_id: 'temp_user_id', // В реальной реализации это будет передаваться
      }),
    });

    if (!response.ok) {
      const error = await response.json();
      return NextResponse.json(
        { error: error.message || 'Verification failed' },
        { status: 400 }
      );
    }

    const data = await response.json();

    return NextResponse.json({
      success: true,
      message: 'Verification successful',
      tokens: data.tokens,
      sessionId: data.session_id,
    });
  } catch (error) {
    console.error('Error during OAuth verification:', error);
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
