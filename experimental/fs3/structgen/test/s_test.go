package test

import "testing"

func TestSimple(t *testing.T) {
	var test Simple

	test.SetA(1)
	if test.GetA() != 1 {
		t.Errorf("Expected 1, got %d", test.GetA())
	}

	test.SetB(2)
	if test.GetB() != 2 {
		t.Errorf("Expected 2, got %d", test.GetB())
	}

	test.SetC(3)
	if test.GetC() != 3 {
		t.Errorf("Expected 3, got %d", test.GetC())
	}

	test.SetD(4)
	if test.GetD() != 4 {
		t.Errorf("Expected 4, got %d", test.GetD())
	}
}

func TestSimple2(t *testing.T) {
	var test Simple2

	b := test.GetA()
	b.SetA(1)
	test.SetA(b)
	if test.GetA().GetA() != 1 {
		t.Errorf("Expected 1, got %d", test.GetA().GetA())
	}

	c := test.GetB()
	c.SetA(2)
	test.SetB(c)
	if test.GetB().GetA() != 2 {
		t.Errorf("Expected 2, got %d", test.GetB().GetA())
	}

	test.SetB(test.GetA())
	if test.GetB().GetA() != 1 {
		t.Errorf("Expected 1, got %d", test.GetB().GetA())
	}
}
