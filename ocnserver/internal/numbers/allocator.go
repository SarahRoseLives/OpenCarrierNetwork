package numbers

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
)

const (
	minNumber = 1000000
	maxNumber = 9999999
)

type Allocator struct {
	areaCode string
	existsFn func(string) (bool, error)
	mu       sync.Mutex
}

func NewAllocator(areaCode string, existsFn func(string) (bool, error)) *Allocator {
	return &Allocator{
		areaCode: areaCode,
		existsFn: existsFn,
	}
}

func (a *Allocator) Allocate() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for i := 0; i < 100; i++ {
		num := minNumber + rand.Intn(maxNumber-minNumber+1)
		numStr := fmt.Sprintf("%07d", num)

		exists, err := a.existsFn(numStr)
		if err != nil {
			return "", fmt.Errorf("checking number existence: %w", err)
		}
		if !exists {
			return numStr, nil
		}
	}

	for num := minNumber; num <= maxNumber; num++ {
		numStr := fmt.Sprintf("%07d", num)
		exists, err := a.existsFn(numStr)
		if err != nil {
			return "", fmt.Errorf("checking number existence: %w", err)
		}
		if !exists {
			return numStr, nil
		}
	}

	return "", fmt.Errorf("no available numbers")
}

// FormatLocal formats a 7-digit number as XXX-XXXX
func FormatLocal(number string) string {
	if len(number) == 7 {
		return number[:3] + "-" + number[3:]
	}
	return number
}

// FormatNumber formats a number with optional area code
func FormatNumber(areaCode, number string) string {
	if areaCode == "" {
		return FormatLocal(number)
	}
	return areaCode + "-" + FormatLocal(number)
}

// ParseNumber parses a dial string. With no area code, accepts 7 digits.
// With area code, accepts 7 or 10 digits.
func ParseNumber(dial string, defaultAreaCode string) (areaCode, localNumber string, err error) {
	clean := strings.ReplaceAll(dial, "-", "")

	switch len(clean) {
	case 7:
		return defaultAreaCode, clean, nil
	case 10:
		if defaultAreaCode == "" {
			// No area code configured — treat 10 digits as area+local
			return clean[:3], clean[3:], nil
		}
		return clean[:3], clean[3:], nil
	default:
		return "", "", fmt.Errorf("invalid number format: %s (expected 7 or 10 digits)", dial)
	}
}
