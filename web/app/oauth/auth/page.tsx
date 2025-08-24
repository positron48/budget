'use client';

import { useState, useEffect, useCallback } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import { Card } from '@/components/Card';
import Button from '@/components/Button';

import LoadingSpinner from '@/components/LoadingSpinner';
import Icon from '@/components/Icon';

interface AuthState {
  step: 'loading' | 'verification' | 'success' | 'error';
  token?: string;
  verificationCode?: string;
  error?: string;
}

export default function OAuthAuthPage() {
  const searchParams = useSearchParams();
  const _router = useRouter();
  const [state, setState] = useState<AuthState>({ step: 'loading' });

  const getVerificationCode = useCallback(async (token: string) => {
    try {
      const response = await fetch('/api/oauth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || 'Failed to get verification code');
      }

      setState({ 
        step: 'verification', 
        token, 
        verificationCode: data.verificationCode 
      });
    } catch (_error) {
      setState({ 
        step: 'error', 
        token,
        error: _error instanceof Error ? _error.message : 'Failed to get verification code' 
      });
    }
  }, []);

  const checkTokenStatus = useCallback(async (token: string) => {
    try {
      const response = await fetch(`/api/oauth/status?token=${token}`);
      const data = await response.json();
      
      if (!response.ok) {
        throw new Error(data.error || 'Failed to check token status');
      }
      
      if (data.status === 'completed') {
        setState({ step: 'success', token });
      } else if (data.status === 'expired') {
        setState({ step: 'error', error: 'Authorization link has expired' });
      } else if (data.status === 'cancelled') {
        setState({ step: 'error', error: 'Authorization was cancelled' });
      } else {
        // Сразу получаем код подтверждения
        getVerificationCode(token);
      }
    } catch (_error) {
      setState({ step: 'error', error: _error instanceof Error ? _error.message : 'Failed to check authorization status' });
    }
  }, [getVerificationCode]);

  useEffect(() => {
    const token = searchParams.get('token');
    if (!token) {
      setState({ step: 'error', error: 'Missing authorization token' });
      return;
    }

    // Проверяем статус токена
    checkTokenStatus(token);
  }, [searchParams, checkTokenStatus]);





  if (state.step === 'loading') {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <Card className="w-full max-w-md p-8">
          <div className="flex flex-col items-center space-y-4">
            <LoadingSpinner size="lg" />
            <p className="text-gray-600">Checking authorization...</p>
          </div>
        </Card>
      </div>
    );
  }

  if (state.step === 'error') {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <Card className="w-full max-w-md p-8">
          <div className="flex flex-col items-center space-y-4">
            <div className="w-16 h-16 bg-red-100 rounded-full flex items-center justify-center">
              <Icon name="alert-circle" className="w-8 h-8 text-red-600" />
            </div>
            <h2 className="text-xl font-semibold text-gray-900">Authorization Failed</h2>
            <p className="text-gray-600 text-center">{state.error}</p>
            <Button onClick={() => window.close()} variant="outline">
              Close Window
            </Button>
          </div>
        </Card>
      </div>
    );
  }

  if (state.step === 'success') {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <Card className="w-full max-w-md p-8">
          <div className="flex flex-col items-center space-y-4">
            <div className="w-16 h-16 bg-green-100 rounded-full flex items-center justify-center">
              <Icon name="check" className="w-8 h-8 text-green-600" />
            </div>
            <h2 className="text-xl font-semibold text-gray-900">Authorization Successful</h2>
            <p className="text-gray-600 text-center">
              You have been successfully authorized. You can now return to Telegram.
            </p>
            <div className="bg-gray-100 p-4 rounded-lg w-full">
              <p className="text-sm text-gray-600 mb-2">Verification Code:</p>
              <p className="text-2xl font-mono font-bold text-center text-gray-900">
                {state.verificationCode}
              </p>
            </div>
            <p className="text-xs text-gray-500 text-center">
              Enter this code in your Telegram bot to complete the authorization.
            </p>
            <Button onClick={() => window.close()} variant="outline">
              Close Window
            </Button>
          </div>
        </Card>
      </div>
    );
  }



  if (state.step === 'verification') {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <Card className="w-full max-w-md p-8">
          <div className="flex flex-col items-center space-y-6">
            <div className="w-16 h-16 bg-yellow-100 rounded-full flex items-center justify-center">
              <Icon name="lock" className="w-8 h-8 text-yellow-600" />
            </div>
            <div className="text-center">
              <h2 className="text-xl font-semibold text-gray-900">Verification Code</h2>
              <p className="text-gray-600 mt-2">
                Enter this code in your Telegram bot to complete the authorization.
              </p>
            </div>

            <div className="bg-gray-100 p-4 rounded-lg w-full">
              <p className="text-sm text-gray-600 mb-2">Verification Code:</p>
              <p className="text-2xl font-mono font-bold text-center text-gray-900">
                {state.verificationCode}
              </p>
            </div>

            <div className="text-xs text-gray-500 text-center">
              Copy this code and paste it in your Telegram bot.
            </div>
          </div>
        </Card>
      </div>
    );
  }

  return null;
}
