package payment

import (
	"testing"
)

func TestFingerprint(t *testing.T) {
	refID := "checkout:123"
	amt, _ := NewAmount(10990)
	curr, _ := NewCurrency("BRL")
	tok, _ := NewScenarioToken("card_approved")

	t.Run("determinism: same values produce same fingerprint", func(t *testing.T) {
		t.Run("determinism for reference_id", func(t *testing.T) {
			// Hold other three fields constant, call twice with same refID
			fp1 := Fingerprint(refID, amt, curr, tok)
			fp2 := Fingerprint(refID, amt, curr, tok)
			if fp1 != fp2 {
				t.Errorf("same reference_id values produced different fingerprints: %x vs %x", fp1, fp2)
			}
		})

		t.Run("determinism for amount", func(t *testing.T) {
			// Hold other three fields constant, call twice with same amount
			fp1 := Fingerprint(refID, amt, curr, tok)
			fp2 := Fingerprint(refID, amt, curr, tok)
			if fp1 != fp2 {
				t.Errorf("same amount values produced different fingerprints: %x vs %x", fp1, fp2)
			}
		})

		t.Run("determinism for currency", func(t *testing.T) {
			// Hold other three fields constant, call twice with same currency
			fp1 := Fingerprint(refID, amt, curr, tok)
			fp2 := Fingerprint(refID, amt, curr, tok)
			if fp1 != fp2 {
				t.Errorf("same currency values produced different fingerprints: %x vs %x", fp1, fp2)
			}
		})

		t.Run("determinism for token", func(t *testing.T) {
			// Hold other three fields constant, call twice with same token
			fp1 := Fingerprint(refID, amt, curr, tok)
			fp2 := Fingerprint(refID, amt, curr, tok)
			if fp1 != fp2 {
				t.Errorf("same token values produced different fingerprints: %x vs %x", fp1, fp2)
			}
		})
	})

	t.Run("value changes: different field values produce different fingerprints", func(t *testing.T) {
		// Create reference fingerprint
		baseFP := Fingerprint(refID, amt, curr, tok)

		t.Run("different reference_id", func(t *testing.T) {
			differentRefID := "checkout:456"
			newFP := Fingerprint(differentRefID, amt, curr, tok)
			if baseFP == newFP {
				t.Errorf("different reference_id produced same fingerprint: %x", baseFP)
			}
		})

		t.Run("different amount", func(t *testing.T) {
			differentAmt, _ := NewAmount(20000)
			newFP := Fingerprint(refID, differentAmt, curr, tok)
			if baseFP == newFP {
				t.Errorf("different amount produced same fingerprint: %x", baseFP)
			}
		})

		t.Run("different token", func(t *testing.T) {
			differentTok, _ := NewScenarioToken("card_declined")
			newFP := Fingerprint(refID, amt, curr, differentTok)
			if baseFP == newFP {
				t.Errorf("different token produced same fingerprint: %x", baseFP)
			}
		})

		t.Run("two different tokens with same amount and reference", func(t *testing.T) {
			tok1, _ := NewScenarioToken("card_approved")
			tok2, _ := NewScenarioToken("card_processing_approved")
			fp1 := Fingerprint(refID, amt, curr, tok1)
			fp2 := Fingerprint(refID, amt, curr, tok2)
			if fp1 == fp2 {
				t.Errorf("different tokens produced same fingerprint: %x", fp1)
			}
		})
	})

	t.Run("returns 32-byte SHA-256 hash", func(t *testing.T) {
		fp := Fingerprint(refID, amt, curr, tok)
		if len(fp) != 32 {
			t.Errorf("fingerprint length: got %d, want 32", len(fp))
		}
	})

	t.Run("empty reference_id produces a valid fingerprint", func(t *testing.T) {
		fp := Fingerprint("", amt, curr, tok)
		if len(fp) != 32 {
			t.Errorf("fingerprint length: got %d, want 32", len(fp))
		}
	})

	t.Run("handles all four scenario tokens", func(t *testing.T) {
		tokens := []ScenarioToken{
			TokenCardApproved,
			TokenCardDeclined,
			TokenCardProcessingApproved,
			TokenCardProcessingDeclined,
		}

		fingerprints := make(map[[32]byte]bool)
		for _, token := range tokens {
			fp := Fingerprint(refID, amt, curr, token)
			if fingerprints[fp] {
				t.Errorf("token %s produced duplicate fingerprint", token)
			}
			fingerprints[fp] = true
		}

		if len(fingerprints) != len(tokens) {
			t.Errorf("not all tokens produced unique fingerprints: got %d unique, want %d", len(fingerprints), len(tokens))
		}
	})
}

// Benchmark for reference
func BenchmarkFingerprint(b *testing.B) {
	refID := "checkout:123"
	amt, _ := NewAmount(10990)
	curr, _ := NewCurrency("BRL")
	tok, _ := NewScenarioToken("card_approved")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Fingerprint(refID, amt, curr, tok)
	}
}
