'use client';

import { useState, useEffect } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import { Card } from '@/components/Card';
import Button from '@/components/Button';
import Input from '@/components/Input';
import LoadingSpinner from '@/components/LoadingSpinner';
import Icon from '@/components/Icon';

interface AuthState {
  step: 'loading' | 'login' | 'verification' | 'success' | 'error';
  token?: string;
  email?: string;
  verificationCode?: string;
  error?: string;
}

export default function OAuthAuthPage() {
  const searchParams = useSearchParams();
  const _router = useRouter();
  const [state, setState] = useState<AuthState>({ step: 'loading' });
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    const token = searchParams.get('token');
    if (!token) {
      setState({ step: 'error', error: 'Missing authorization token' });
      return;
    }

    // Проверяем статус токена
    checkTokenStatus(token);
  }, [searchParams]);

  const checkTokenStatus = async (token: string) => {
    try {
      const response = await fetch(`/api/oauth/status?token=${token}`);
      if (!response.ok) {
        throw new Error('Failed to check token status');
      }

      const data = await response.json();
      
      if (data.status === 'completed') {
        setState({ step: 'success', token });
      } else if (data.status === 'expired') {
        setState({ step: 'error', error: 'Authorization link has expired' });
      } else if (data.status === 'cancelled') {
        setState({ step: 'error', error: 'Authorization was cancelled' });
      } else {
        setState({ step: 'login', token });
      }
    } catch (_error) {
      setState({ step: 'error', error: 'Failed to check authorization status' });
    }
  };

  const handleLogin = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!state.token) return;

    setIsSubmitting(true);
    const formData = new FormData(e.currentTarget);
    const email = formData.get('email') as string;
    const password = formData.get('password') as string;

    try {
      const response = await fetch('/api/oauth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: state.token, email, password }),
      });

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.message || 'Login failed');
      }

      const data = await response.json();
      setState({ 
        step: 'verification', 
        token: state.token, 
        email,
        verificationCode: data.verificationCode 
      });
    } catch (_error) {
      setState({ 
        step: 'error', 
        token: state.token,
        error: _error instanceof Error ? _error.message : 'Login failed' 
      });
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleVerification = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!state.token || !state.verificationCode) return;

    setIsSubmitting(true);

    try {
      const response = await fetch('/api/oauth/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ 
          token: state.token, 
          verificationCode: state.verificationCode 
        }),
      });

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.message || 'Verification failed');
      }

      setState({ step: 'success', token: state.token });
    } catch (_error) {
      setState({ 
        step: 'error', 
        token: state.token,
        error: _error instanceof Error ? _error.message : 'Verification failed' 
      });
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleCancel = async () => {
    if (!state.token) return;

    try {
      await fetch('/api/oauth/cancel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: state.token }),
      });
    } catch (_error) {
      console.error('Failed to cancel authorization:', _error);
    }

    setState({ step: 'error', error: 'Authorization cancelled' });
  };

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

  if (state.step === 'login') {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <Card className="w-full max-w-md p-8">
          <div className="flex flex-col items-center space-y-6">
            <div className="w-16 h-16 bg-blue-100 rounded-full flex items-center justify-center">
              <Icon name="lock" className="w-8 h-8 text-blue-600" />
            </div>
            <div className="text-center">
              <h2 className="text-xl font-semibold text-gray-900">Authorize Telegram Bot</h2>
              <p className="text-gray-600 mt-2">
                Please log in to authorize access to your budget data.
              </p>
            </div>

            <form onSubmit={handleLogin} className="w-full space-y-4">
              <div>
                <label htmlFor="email" className="block text-sm font-medium text-gray-700 mb-1">
                  Email
                </label>
                <Input
                  id="email"
                  name="email"
                  type="email"
                  required
                  placeholder="Enter your email"
                  className="w-full"
                />
              </div>

              <div>
                <label htmlFor="password" className="block text-sm font-medium text-gray-700 mb-1">
                  Password
                </label>
                <Input
                  id="password"
                  name="password"
                  type="password"
                  required
                  placeholder="Enter your password"
                  className="w-full"
                />
              </div>

              <div className="flex space-x-3">
                <Button
                  type="submit"
                  disabled={isSubmitting}
                  className="flex-1"
                >
                  {isSubmitting ? <LoadingSpinner size="sm" /> : 'Authorize'}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleCancel}
                  disabled={isSubmitting}
                >
                  Cancel
                </Button>
              </div>
            </form>

            <div className="text-xs text-gray-500 text-center">
              By authorizing, you allow the Telegram bot to access your budget data.
            </div>
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
              <h2 className="text-xl font-semibold text-gray-900">Verification Required</h2>
              <p className="text-gray-600 mt-2">
                Please enter the verification code to complete authorization.
              </p>
            </div>

            <div className="bg-gray-100 p-4 rounded-lg w-full">
              <p className="text-sm text-gray-600 mb-2">Verification Code:</p>
              <p className="text-2xl font-mono font-bold text-center text-gray-900">
                {state.verificationCode}
              </p>
            </div>

            <form onSubmit={handleVerification} className="w-full space-y-4">
              <div>
                <label htmlFor="verificationCode" className="block text-sm font-medium text-gray-700 mb-1">
                  Verification Code
                </label>
                <Input
                  id="verificationCode"
                  name="verificationCode"
                  type="text"
                  required
                  placeholder="Enter verification code"
                  className="w-full"
                  defaultValue={state.verificationCode}
                />
              </div>

              <div className="flex space-x-3">
                <Button
                  type="submit"
                  disabled={isSubmitting}
                  className="flex-1"
                >
                  {isSubmitting ? <LoadingSpinner size="sm" /> : 'Verify'}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleCancel}
                  disabled={isSubmitting}
                >
                  Cancel
                </Button>
              </div>
            </form>

            <div className="text-xs text-gray-500 text-center">
              Enter this code in your Telegram bot to complete the authorization.
            </div>
          </div>
        </Card>
      </div>
    );
  }

  return null;
}
