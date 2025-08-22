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

    // TODO: Реализовать вызов gRPC сервиса для отмены
    const response = await fetch(`${process.env.GRPC_GATEWAY_URL}/v1/oauth/cancel`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        auth_token: token,
        telegram_user_id: 'temp_user_id', // В реальной реализации это будет передаваться
      }),
    });

    if (!response.ok) {
      const error = await response.json();
      return NextResponse.json(
        { error: error.message || 'Failed to cancel authorization' },
        { status: 400 }
      );
    }

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
