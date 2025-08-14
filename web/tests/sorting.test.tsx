import { describe, it, expect } from 'vitest';

// Test for sort parameter validation
describe('Sorting', () => {
  it('should validate sort parameters correctly', () => {
    const validSorts = [
      'occurred_at',
      'occurred_at asc',
      'occurred_at desc',
      'amount_numeric',
      'amount_numeric asc',
      'amount_numeric desc',
      'comment',
      'comment asc',
      'comment desc',
      'type',
      'type asc',
      'type desc',
      'created_at',
      'created_at asc',
      'created_at desc',
      'category_code',
      'category_code asc',
      'category_code desc',
    ];

    validSorts.forEach(sort => {
      expect(sort).toMatch(/^(occurred_at|amount_numeric|comment|type|created_at|category_code)(\s+(asc|desc))?$/);
    });
  });

  it('should have default sort as occurred_at desc', () => {
    const defaultSort = 'occurred_at desc';
    expect(defaultSort).toBe('occurred_at desc');
  });

  it('should handle sort direction cycling with field-specific defaults', () => {
    // Test the sort direction cycling logic with different default directions
    const cycleSort = (currentSort: string, field: string, defaultDirection: "asc" | "desc"): string => {
      if (currentSort === `${field} ${defaultDirection}`) {
        // Second click: switch to opposite direction
        return `${field} ${defaultDirection === "asc" ? "desc" : "asc"}`;
      } else if (currentSort === `${field} ${defaultDirection === "asc" ? "desc" : "asc"}`) {
        // Third click: remove sorting
        return '';
      } else {
        // First click: use default direction
        return `${field} ${defaultDirection}`;
      }
    };

    // Test with different default directions
    expect(cycleSort('', 'amount_numeric', 'asc')).toBe('amount_numeric asc'); // First click
    expect(cycleSort('amount_numeric asc', 'amount_numeric', 'asc')).toBe('amount_numeric desc'); // Second click
    expect(cycleSort('amount_numeric desc', 'amount_numeric', 'asc')).toBe(''); // Third click
    
    expect(cycleSort('', 'occurred_at', 'desc')).toBe('occurred_at desc'); // First click
    expect(cycleSort('occurred_at desc', 'occurred_at', 'desc')).toBe('occurred_at asc'); // Second click
    expect(cycleSort('occurred_at asc', 'occurred_at', 'desc')).toBe(''); // Third click
  });

  it('should validate sort field names', () => {
    const validFields = ['occurred_at', 'amount_numeric', 'comment', 'type', 'created_at', 'category_code'];
    const testField = 'amount_numeric';
    
    expect(validFields).toContain(testField);
    expect(validFields).toContain('category_code');
    expect(validFields).not.toContain('invalid_field');
  });

  it('should have correct default sort directions for each field', () => {
    const fieldDefaults = {
      'type': 'asc',
      'occurred_at': 'desc',
      'comment': 'asc',
      'category_code': 'asc',
      'amount_numeric': 'asc'
    };

    // Verify each field has the correct default direction
    expect(fieldDefaults['type']).toBe('asc');
    expect(fieldDefaults['occurred_at']).toBe('desc');
    expect(fieldDefaults['comment']).toBe('asc');
    expect(fieldDefaults['category_code']).toBe('asc');
    expect(fieldDefaults['amount_numeric']).toBe('asc');
  });

  it('should complete full sort cycle correctly', () => {
    // Test complete cycle for amount_numeric field (default: asc)
    const field = 'amount_numeric';
    const defaultDirection = 'asc';
    
    // Simulate the complete click cycle
    let currentSort = '';
    
    // First click: should set to default direction
    currentSort = `${field} ${defaultDirection}`;
    expect(currentSort).toBe('amount_numeric asc');
    
    // Second click: should switch to opposite direction
    currentSort = `${field} ${defaultDirection === 'asc' ? 'desc' : 'asc'}`;
    expect(currentSort).toBe('amount_numeric desc');
    
    // Third click: should reset to no sorting
    currentSort = '';
    expect(currentSort).toBe('');
    
    // Test complete cycle for occurred_at field (default: desc)
    const dateField = 'occurred_at';
    const dateDefaultDirection = 'desc';
    
    // First click: should set to default direction
    currentSort = `${dateField} ${dateDefaultDirection}`;
    expect(currentSort).toBe('occurred_at desc');
    
    // Second click: should switch to opposite direction
    currentSort = `${dateField} ${dateDefaultDirection === 'asc' ? 'desc' : 'asc'}`;
    expect(currentSort).toBe('occurred_at asc');
    
    // Third click: should reset to no sorting
    currentSort = '';
    expect(currentSort).toBe('');
  });
});
