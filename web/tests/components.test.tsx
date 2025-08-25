import React from "react";
import { screen, fireEvent, render } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { renderWithIntl } from "./utils";

// Import components
import { Button, Input, Icon, Badge, Card, CardTitle, LoadingSpinner } from "@/components";

describe('Components', () => {
  describe('Button', () => {
    it('renders button with text', () => {
      render(<Button>Click me</Button>);
      expect(screen.getByText('Click me')).toBeInTheDocument();
    });

    it('handles click events', () => {
      const handleClick = vi.fn();
      render(<Button onClick={handleClick}>Click me</Button>);
      
      fireEvent.click(screen.getByText('Click me'));
      expect(handleClick).toHaveBeenCalledTimes(1);
    });

    it('renders with different variants', () => {
      const { rerender } = render(<Button variant="primary">Primary</Button>);
      expect(screen.getByText('Primary')).toHaveClass('btn-primary');

      rerender(<Button variant="secondary">Secondary</Button>);
      expect(screen.getByText('Secondary')).toHaveClass('btn-secondary');

      rerender(<Button variant="outline">Outline</Button>);
      expect(screen.getByText('Outline')).toHaveClass('btn-outline');
    });

    it('renders with different sizes', () => {
      const { rerender } = render(<Button size="sm">Small</Button>);
      expect(screen.getByText('Small')).toHaveClass('text-xs');

      rerender(<Button size="lg">Large</Button>);
      expect(screen.getByText('Large')).toHaveClass('text-lg');
    });

    it('shows loading state', () => {
      render(<Button loading>Loading</Button>);
      expect(screen.getByText('Loading')).toBeDisabled();
    });

    it('can be disabled', () => {
      render(<Button disabled>Disabled</Button>);
      expect(screen.getByText('Disabled')).toBeDisabled();
    });
  });

  describe('Input', () => {
    it('renders input with label', () => {
      render(<Input label="Email" placeholder="Enter email" />);
      expect(screen.getByText('Email')).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Enter email')).toBeInTheDocument();
    });

    it('handles value changes', () => {
      const handleChange = vi.fn();
      render(<Input onChange={handleChange} placeholder="Test" />);
      
      const input = screen.getByPlaceholderText('Test');
      fireEvent.change(input, { target: { value: 'test@example.com' } });
      
      expect(handleChange).toHaveBeenCalled();
    });

    it('shows error state', () => {
      render(<Input error="This field is required" placeholder="Test" />);
      expect(screen.getByText('This field is required')).toBeInTheDocument();
    });

    it('can be disabled', () => {
      render(<Input disabled placeholder="Test" />);
      expect(screen.getByPlaceholderText('Test')).toBeDisabled();
    });
  });

  describe('Icon', () => {
    it('renders icon with correct name', () => {
      render(<Icon name="plus" />);
      const icon = screen.getByRole('img', { hidden: true });
      expect(icon).toBeInTheDocument();
    });

    it('renders with different sizes', () => {
      const { rerender } = render(<Icon name="plus" size={16} />);
      let icon = screen.getByRole('img', { hidden: true });
      expect(icon).toHaveAttribute('width', '16');
      expect(icon).toHaveAttribute('height', '16');

      rerender(<Icon name="plus" size={24} />);
      icon = screen.getByRole('img', { hidden: true });
      expect(icon).toHaveAttribute('width', '24');
      expect(icon).toHaveAttribute('height', '24');
    });

    it('applies custom className', () => {
      render(<Icon name="plus" className="custom-class" />);
      const icon = screen.getByRole('img', { hidden: true });
      expect(icon).toHaveClass('custom-class');
    });
  });

  describe('Badge', () => {
    it('renders badge with text', () => {
      render(<Badge>New</Badge>);
      expect(screen.getByText('New')).toBeInTheDocument();
    });

    it('renders with different variants', () => {
      const { rerender } = render(<Badge variant="primary">Primary</Badge>);
      expect(screen.getByText('Primary')).toHaveClass('bg-blue-100', 'text-blue-800');

      rerender(<Badge variant="success">Success</Badge>);
      expect(screen.getByText('Success')).toHaveClass('bg-green-100', 'text-green-800');

      rerender(<Badge variant="warning">Warning</Badge>);
      expect(screen.getByText('Warning')).toHaveClass('bg-yellow-100', 'text-yellow-800');

      rerender(<Badge variant="danger">Danger</Badge>);
      expect(screen.getByText('Danger')).toHaveClass('bg-red-100', 'text-red-800');
    });
  });

  describe('Card', () => {
    it('renders card with content', () => {
      render(
        <Card>
          <div>Card content</div>
        </Card>
      );
      expect(screen.getByText('Card content')).toBeInTheDocument();
    });

    it('renders card with title using CardTitle component', () => {
      render(
        <Card>
          <CardTitle>Card Title</CardTitle>
          <div>Card content</div>
        </Card>
      );
      expect(screen.getByText('Card Title')).toBeInTheDocument();
    });
  });

  describe('LoadingSpinner', () => {
    it('renders loading spinner', () => {
      render(<LoadingSpinner />);
      const spinner = screen.getByRole('img', { hidden: true });
      expect(spinner).toBeInTheDocument();
    });

    it('renders with custom size', () => {
      render(<LoadingSpinner size="lg" />);
      const spinner = screen.getByRole('img', { hidden: true });
      expect(spinner).toHaveAttribute('width', '32');
      expect(spinner).toHaveAttribute('height', '32');
    });

    it('renders with custom text', () => {
      render(<LoadingSpinner text="Loading data..." />);
      expect(screen.getByText('Loading data...')).toBeInTheDocument();
    });
  });
});
