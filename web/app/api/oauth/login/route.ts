import { NextRequest, NextResponse } from 'next/server';

export async function POST(request: NextRequest) {
  try {
    const body = await request.json();
    const { token, email, password } = body;

    if (!token || !email || !password) {
      return NextResponse.json(
        { error: 'Token, email and password are required' },
        { status: 400 }
      );
    }

    // TODO: Реализовать вызов gRPC сервиса для логина
    // Пока возвращаем заглушку с тестовым кодом
    const verificationCode = Math.floor(100000 + Math.random() * 900000).toString();

    // В реальной реализации здесь будет вызов gRPC сервиса
    // для проверки логина и получения кода подтверждения

    return NextResponse.json({
      verificationCode,
      message: 'Login successful, verification code generated',
    });
  } catch (error) {
    console.error('Error during OAuth login:', error);
    return NextResponse.json(
      { error: 'Internal server error' },
      { status: 500 }
    );
  }
}
