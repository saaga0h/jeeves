package light

// BrightnessResult contains the calculated brightness and reasoning
type BrightnessResult struct {
	Brightness int
	Reason     string
}

// calculateBrightness determines the appropriate brightness level based on illuminance conditions
func calculateBrightness(illuminanceState string, lux float64, isNaturalLight bool, timeOfDay string) BrightnessResult {
	// Late hours (when we want dimmer lights)
	isLateHours := timeOfDay == "night" || timeOfDay == "late_evening"

	// Dark conditions (< 5 lux)
	if illuminanceState == "dark" || lux < 5 {
		if isLateHours {
			return BrightnessResult{
				Brightness: 50,
				Reason:     "dark conditions, late hours - dim",
			}
		}
		return BrightnessResult{
			Brightness: 80,
			Reason:     "dark conditions, active hours - bright",
		}
	}

	// Dim conditions (5-50 lux)
	if illuminanceState == "dim" || (lux >= 5 && lux < 50) {
		if isLateHours {
			return BrightnessResult{
				Brightness: 40,
				Reason:     "dim conditions, late hours - lower",
			}
		}
		return BrightnessResult{
			Brightness: 60,
			Reason:     "dim conditions, active hours - moderate",
		}
	}

	// Moderate conditions (50-200 lux)
	if illuminanceState == "moderate" || (lux >= 50 && lux < 200) {
		if isNaturalLight {
			return BrightnessResult{
				Brightness: 20,
				Reason:     "moderate natural light - minimal supplement",
			}
		}
		return BrightnessResult{
			Brightness: 40,
			Reason:     "moderate artificial light - more supplement needed",
		}
	}

	// Bright conditions (> 200 lux)
	if illuminanceState == "bright" || lux >= 200 {
		if isNaturalLight {
			return BrightnessResult{
				Brightness: 0,
				Reason:     "bright natural light - no artificial light needed",
			}
		}
		return BrightnessResult{
			Brightness: 10,
			Reason:     "bright artificial light - minimal addition",
		}
	}

	// Fallback for unknown illuminance state
	return BrightnessResult{
		Brightness: 50,
		Reason:     "unknown illuminance - default moderate",
	}
}
