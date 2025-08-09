package logger

import "testing"

func TestNew_Dev(t *testing.T) {
	lg, err := New("dev")
	if err != nil || lg == nil {
		t.Fatalf("dev logger: %v %v", err, lg)
	}
	_ = lg.Sync()
}

func TestNew_Prod(t *testing.T) {
	lg, err := New("prod")
	if err != nil || lg == nil {
		t.Fatalf("prod logger: %v %v", err, lg)
	}
	_ = lg.Sync()
}
