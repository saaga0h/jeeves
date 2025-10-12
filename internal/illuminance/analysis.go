package illuminance

import (
	"fmt"
	"math"
	"time"

	"github.com/sixdouglas/suncalc"
)

// IlluminanceAbstraction represents the complete temporal abstraction
type IlluminanceAbstraction struct {
	Current struct {
		Lux       float64
		Label     string
		Timestamp time.Time
	}
	TemporalPatterns struct {
		Immediate  string
		ShortTerm  string
		MediumTerm string
		LongTerm   string
	}
	TemporalAnalysis struct {
		Trend2Min   string
		Trend10Min  string
		Trend30Min  string
		Stability   string
	}
	Statistics struct {
		Avg2Min    float64
		Avg10Min   float64
		Min10Min   float64
		Max10Min   float64
		Variation  float64
	}
	Daylight struct {
		TheoreticalOutdoorLux float64
		SunAltitude           float64
		IsDaytime             bool
		IsGoldenHour          bool
	}
	Context struct {
		TimeOfDay          string
		LikelySources      []string
		RelativeToTypical  string
	}
	DataSource string
	DataAge    time.Duration
}

// GenerateIlluminanceAbstraction creates a complete temporal abstraction
func GenerateIlluminanceAbstraction(summary *DataSummary, lat, lon float64) (*IlluminanceAbstraction, error) {
	if summary.LatestReading == nil {
		return nil, fmt.Errorf("no latest reading available")
	}

	now := time.Now()
	abstraction := &IlluminanceAbstraction{}

	// Set current values
	abstraction.Current.Lux = summary.LatestReading.Lux
	abstraction.Current.Label = LuxToLabel(summary.LatestReading.Lux)
	abstraction.Current.Timestamp = summary.LatestReading.Timestamp

	// Calculate data age
	abstraction.DataAge = now.Sub(summary.LatestReading.Timestamp)

	// Calculate daylight context
	abstraction.Daylight = calculateDaylightContext(lat, lon, now)

	// Analyze time windows
	window2Min := AnalyzeWindow(summary.Last5Min, 2, now)
	window10Min := AnalyzeWindow(summary.Last30Min, 10, now)
	window30Min := AnalyzeWindow(summary.LastHour, 30, now)

	// Build temporal patterns (using averages when available, current as fallback)
	abstraction.TemporalPatterns.Immediate = abstraction.Current.Label

	if window2Min.Count > 0 {
		abstraction.TemporalPatterns.ShortTerm = window2Min.Label
	} else {
		abstraction.TemporalPatterns.ShortTerm = abstraction.Current.Label
	}

	if window10Min.Count > 0 {
		abstraction.TemporalPatterns.MediumTerm = window10Min.Label
	} else {
		abstraction.TemporalPatterns.MediumTerm = abstraction.Current.Label
	}

	if window30Min.Count > 0 {
		abstraction.TemporalPatterns.LongTerm = window30Min.Label
	} else {
		abstraction.TemporalPatterns.LongTerm = abstraction.Current.Label
	}

	// Build temporal analysis
	abstraction.TemporalAnalysis.Trend2Min = window2Min.Trend
	abstraction.TemporalAnalysis.Trend10Min = window10Min.Trend
	abstraction.TemporalAnalysis.Trend30Min = window30Min.Trend
	abstraction.TemporalAnalysis.Stability = window10Min.Stability

	// Build statistics
	abstraction.Statistics.Avg2Min = window2Min.AverageLux
	if abstraction.Statistics.Avg2Min == 0 {
		abstraction.Statistics.Avg2Min = abstraction.Current.Lux
	}

	abstraction.Statistics.Avg10Min = window10Min.AverageLux
	if abstraction.Statistics.Avg10Min == 0 {
		abstraction.Statistics.Avg10Min = abstraction.Current.Lux
	}

	abstraction.Statistics.Min10Min = window10Min.MinLux
	if abstraction.Statistics.Min10Min == 0 && window10Min.Count == 0 {
		abstraction.Statistics.Min10Min = abstraction.Current.Lux
	}

	abstraction.Statistics.Max10Min = window10Min.MaxLux
	if abstraction.Statistics.Max10Min == 0 && window10Min.Count == 0 {
		abstraction.Statistics.Max10Min = abstraction.Current.Lux
	}

	abstraction.Statistics.Variation = window10Min.MaxLux - window10Min.MinLux

	// Build context
	currentHour := now.Hour()
	abstraction.Context.TimeOfDay = GetTimeOfDay(currentHour)
	abstraction.Context.LikelySources = DetermineLightSource(
		abstraction.Current.Lux,
		abstraction.Daylight.IsDaytime,
		abstraction.Daylight.TheoreticalOutdoorLux,
	)
	abstraction.Context.RelativeToTypical = CompareToTypical(
		abstraction.Current.Lux,
		abstraction.Context.TimeOfDay,
	)

	// Determine data source
	if summary.HasSufficientData {
		abstraction.DataSource = "redis_sensor_data"
	} else {
		abstraction.DataSource = "daylight_calculation"
	}

	return abstraction, nil
}

// calculateDaylightContext calculates sun position and theoretical outdoor lux
func calculateDaylightContext(lat, lon float64, t time.Time) struct {
	TheoreticalOutdoorLux float64
	SunAltitude           float64
	IsDaytime             bool
	IsGoldenHour          bool
} {
	// Get sun position
	position := suncalc.GetPosition(t, lat, lon)

	// Get sun times for the day
	times := suncalc.GetTimes(t, lat, lon)

	// Calculate theoretical outdoor lux based on sun altitude
	// Sun altitude is in radians, convert to degrees
	altitudeDegrees := position.Altitude * (180.0 / math.Pi)

	var theoreticalLux float64
	if altitudeDegrees > 0 {
		// Rough approximation: lux increases with sun altitude
		// At sun altitude of 90° (overhead), theoretical max is ~120,000 lux
		// This is a simplified model
		theoreticalLux = 120000.0 * math.Sin(position.Altitude)
		if theoreticalLux < 0 {
			theoreticalLux = 0
		}
	} else {
		theoreticalLux = 0
	}

	// Determine if it's daytime (sun above horizon)
	isDaytime := altitudeDegrees > 0

	// Check if it's golden hour (sun altitude between 0 and 6 degrees)
	isGoldenHour := altitudeDegrees > 0 && altitudeDegrees < 6

	// Suppress unused variable warning
	_ = times

	return struct {
		TheoreticalOutdoorLux float64
		SunAltitude           float64
		IsDaytime             bool
		IsGoldenHour          bool
	}{
		TheoreticalOutdoorLux: theoreticalLux,
		SunAltitude:           altitudeDegrees,
		IsDaytime:             isDaytime,
		IsGoldenHour:          isGoldenHour,
	}
}
