// Package agent implements the ReAct (Reasoning + Acting) agent loop and all
// tools it can invoke. Tools are defined here; the loop itself lives in agent.go.
package agent

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/expr-lang/expr"
)

// ── Tool interface ─────────────────────────────────────────────────────────────

// Tool is the interface that every agent tool must satisfy.
// Adding a new capability to the agent means implementing this interface and
// registering the tool in Registry() -- nothing else needs to change.
type Tool interface {
	// Name returns the lowercase identifier the model uses in "Action: <name>".
	Name() string
	// Description returns a one-line description included in the system prompt
	// so the model knows when and how to use this tool.
	Description() string
	// Run executes the tool with the given input and returns a result string.
	// Errors are returned as (result, err) so the agent can report them as
	// Observations rather than crashing the loop.
	Run(input string) (string, error)
}

// Registry returns all available tools keyed by their Name().
// The agent loop uses this map to dispatch Action calls.
func Registry() map[string]Tool {
	tools := []Tool{
		&Calculator{},
		&Datetime{},
		&UnitConverter{},
	}
	m := make(map[string]Tool, len(tools))
	for _, t := range tools {
		m[t.Name()] = t
	}
	return m
}

// ── Calculator ─────────────────────────────────────────────────────────────────

// Calculator evaluates arithmetic expressions safely using github.com/expr-lang/expr.
// It exposes common math functions so the model can use sqrt, pow, round, etc.
type Calculator struct{}

func (c *Calculator) Name() string { return "calculator" }
func (c *Calculator) Description() string {
	return "Evaluates math expressions. Supports +, -, *, /, sqrt(), abs(), pow(), round(), pi, e. Example: '200 * 0.30' or 'sqrt(144)'"
}

// Run compiles and evaluates the expression, then formats the result cleanly.
// Whole-number floats (e.g. 4.0) are returned as integers ("4") to avoid
// confusing output like "sqrt(144) = 12.0".
func (c *Calculator) Run(input string) (string, error) {
	// Build the environment: named constants and math functions the model can call.
	env := map[string]any{
		"sqrt":  func(x float64) float64 { return math.Sqrt(x) },
		"abs":   func(x float64) float64 { return math.Abs(x) },
		"pow":   func(x, y float64) float64 { return math.Pow(x, y) },
		"log":   func(x float64) float64 { return math.Log(x) },
		"log10": func(x float64) float64 { return math.Log10(x) },
		"ceil":  func(x float64) float64 { return math.Ceil(x) },
		"floor": func(x float64) float64 { return math.Floor(x) },
		"round": func(x float64) float64 { return math.Round(x) },
		"pi":    math.Pi,
		"e":     math.E,
	}

	result, err := expr.Eval(input, env)
	if err != nil {
		return "", fmt.Errorf("expression error: %w", err)
	}

	return formatNumber(result), nil
}

// formatNumber converts a numeric result to a clean string.
// Whole-number floats drop the decimal: 12.0 -> "12".
func formatNumber(v any) string {
	switch n := v.(type) {
	case float64:
		// If the float is a whole number, return it as an integer string.
		if n == math.Trunc(n) && !math.IsInf(n, 0) && !math.IsNaN(n) {
			return strconv.FormatInt(int64(n), 10)
		}
		// Otherwise use the shortest decimal representation.
		return strconv.FormatFloat(n, 'f', -1, 64)
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case bool:
		return strconv.FormatBool(n)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ── Datetime ───────────────────────────────────────────────────────────────────

// Datetime returns current date and time information using Go's standard
// time package. No external packages required.
type Datetime struct{}

func (d *Datetime) Name() string { return "datetime" }
func (d *Datetime) Description() string {
	return "Returns current date and time. Input: 'now', 'date', 'time', 'day', 'year', or 'timestamp'"
}

// Run returns the requested time component. Unknown inputs fall back to the
// full formatted string so the model always gets a useful observation.
func (d *Datetime) Run(input string) (string, error) {
	now := time.Now()
	switch strings.TrimSpace(strings.ToLower(input)) {
	case "now", "":
		// Full human-readable string with weekday, date, and time.
		return now.Format("Monday, January 2, 2006 at 3:04 PM MST"), nil
	case "date":
		return now.Format("January 2, 2006"), nil
	case "time":
		return now.Format("3:04:05 PM MST"), nil
	case "day":
		return now.Weekday().String(), nil
	case "year":
		return strconv.Itoa(now.Year()), nil
	case "timestamp":
		return strconv.FormatInt(now.Unix(), 10), nil
	default:
		// Unknown input -- return the full datetime so the agent still gets data.
		return now.Format("Monday, January 2, 2006 at 3:04 PM MST"), nil
	}
}

// ── UnitConverter ──────────────────────────────────────────────────────────────

// UnitConverter converts between common units of temperature, length, weight,
// and speed. All logic is hand-written; no external packages are used.
//
// Input format: "VALUE UNIT to UNIT"
// Example: "100 fahrenheit to celsius"  or  "5 miles to kilometers"
type UnitConverter struct{}

func (u *UnitConverter) Name() string { return "unit_convert" }
func (u *UnitConverter) Description() string {
	return "Converts between units. Format: 'VALUE UNIT to UNIT'. Examples: '100 fahrenheit to celsius', '5 miles to kilometers', '10 kg to pounds'"
}

// Run parses the input string, selects the right conversion category, and
// returns the formatted result.
func (u *UnitConverter) Run(input string) (string, error) {
	// Split on " to " to separate "VALUE UNIT" from "TARGET_UNIT".
	parts := strings.SplitN(strings.ToLower(input), " to ", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid format: use 'VALUE UNIT to UNIT', e.g. '5 miles to kilometers'")
	}

	targetUnit := strings.TrimSpace(parts[1])

	// Parse value and source unit from the left side.
	leftFields := strings.Fields(parts[0])
	if len(leftFields) < 2 {
		return "", fmt.Errorf("invalid format: expected 'VALUE UNIT' before 'to'")
	}

	valueStr := leftFields[0]
	sourceUnit := strings.Join(leftFields[1:], " ") // handles units like "km/h"

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return "", fmt.Errorf("invalid number %q: %w", valueStr, err)
	}

	// Dispatch to the correct category based on source unit.
	switch unitCategory(sourceUnit) {
	case "temperature":
		result, err := convertTemperature(value, sourceUnit, targetUnit)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%.4g %s", result, expandUnit(targetUnit)), nil

	case "length":
		result, err := convertViaBase(value, sourceUnit, targetUnit, lengthToBase, lengthFromBase)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%.6g %s", result, expandUnit(targetUnit)), nil

	case "weight":
		result, err := convertViaBase(value, sourceUnit, targetUnit, weightToBase, weightFromBase)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%.6g %s", result, expandUnit(targetUnit)), nil

	case "speed":
		result, err := convertViaBase(value, sourceUnit, targetUnit, speedToBase, speedFromBase)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%.6g %s", result, expandUnit(targetUnit)), nil

	default:
		return "", fmt.Errorf("unknown unit %q -- supported: temperature, length, weight, speed", sourceUnit)
	}
}

// unitCategory returns the measurement category for a unit alias string.
func unitCategory(u string) string {
	switch u {
	case "celsius", "fahrenheit", "kelvin", "c", "f", "k", "°c", "°f", "°k":
		return "temperature"
	case "meters", "meter", "m",
		"kilometers", "kilometer", "km",
		"miles", "mile", "mi",
		"feet", "foot", "ft",
		"inches", "inch", "in",
		"centimeters", "centimeter", "cm":
		return "length"
	case "grams", "gram", "g",
		"kilograms", "kilogram", "kg",
		"pounds", "pound", "lb", "lbs",
		"ounces", "ounce", "oz":
		return "weight"
	case "km/h", "kph", "kmh", "kilometers per hour",
		"miles/h", "mph", "miles per hour",
		"m/s", "meters per second":
		return "speed"
	}
	return ""
}

// expandUnit returns a human-friendly unit name for display in results.
func expandUnit(u string) string {
	switch u {
	case "c", "°c":
		return "Celsius"
	case "f", "°f":
		return "Fahrenheit"
	case "k", "°k":
		return "Kelvin"
	case "m":
		return "meters"
	case "km":
		return "kilometers"
	case "mi":
		return "miles"
	case "ft":
		return "feet"
	case "in":
		return "inches"
	case "cm":
		return "centimeters"
	case "g":
		return "grams"
	case "kg":
		return "kilograms"
	case "lb", "lbs":
		return "pounds"
	case "oz":
		return "ounces"
	case "kph", "kmh":
		return "km/h"
	case "mph":
		return "mph"
	case "m/s":
		return "m/s"
	default:
		return u
	}
}

// ── Temperature (special case: not multiplicative) ─────────────────────────────

// convertTemperature handles the three-way temperature conversion.
// Unlike length/weight/speed, temperature uses offset formulas, not ratios,
// so it cannot use the generic convertViaBase helper.
func convertTemperature(value float64, from, to string) (float64, error) {
	from = normalizeTemp(from)
	to = normalizeTemp(to)

	if from == to {
		return value, nil
	}

	// Step 1: convert source to Celsius as the intermediate unit.
	var celsius float64
	switch from {
	case "celsius":
		celsius = value
	case "fahrenheit":
		celsius = (value - 32) * 5 / 9
	case "kelvin":
		celsius = value - 273.15
	default:
		return 0, fmt.Errorf("unknown temperature unit %q", from)
	}

	// Step 2: convert Celsius to target unit.
	switch to {
	case "celsius":
		return celsius, nil
	case "fahrenheit":
		return celsius*9/5 + 32, nil
	case "kelvin":
		return celsius + 273.15, nil
	default:
		return 0, fmt.Errorf("unknown temperature unit %q", to)
	}
}

// normalizeTemp maps aliases to canonical names.
func normalizeTemp(u string) string {
	switch u {
	case "c", "°c":
		return "celsius"
	case "f", "°f":
		return "fahrenheit"
	case "k", "°k":
		return "kelvin"
	}
	return u
}

// ── Multiplicative unit conversion helpers ─────────────────────────────────────

// convertViaBase converts value from sourceUnit to targetUnit by going through
// a base unit: value -> base -> target. This works for any unit system where
// the relationship is purely multiplicative (length, weight, speed).
func convertViaBase(
	value float64,
	sourceUnit, targetUnit string,
	toBase func(float64, string) (float64, error),
	fromBase func(float64, string) (float64, error),
) (float64, error) {
	base, err := toBase(value, sourceUnit)
	if err != nil {
		return 0, err
	}
	return fromBase(base, targetUnit)
}

// ── Length (base unit: meters) ─────────────────────────────────────────────────

func lengthToBase(value float64, unit string) (float64, error) {
	switch unit {
	case "meters", "meter", "m":
		return value, nil
	case "kilometers", "kilometer", "km":
		return value * 1000, nil
	case "miles", "mile", "mi":
		return value * 1609.344, nil
	case "feet", "foot", "ft":
		return value * 0.3048, nil
	case "inches", "inch", "in":
		return value * 0.0254, nil
	case "centimeters", "centimeter", "cm":
		return value * 0.01, nil
	}
	return 0, fmt.Errorf("unknown length unit %q", unit)
}

func lengthFromBase(meters float64, unit string) (float64, error) {
	switch unit {
	case "meters", "meter", "m":
		return meters, nil
	case "kilometers", "kilometer", "km":
		return meters / 1000, nil
	case "miles", "mile", "mi":
		return meters / 1609.344, nil
	case "feet", "foot", "ft":
		return meters / 0.3048, nil
	case "inches", "inch", "in":
		return meters / 0.0254, nil
	case "centimeters", "centimeter", "cm":
		return meters / 0.01, nil
	}
	return 0, fmt.Errorf("unknown length unit %q", unit)
}

// ── Weight (base unit: grams) ──────────────────────────────────────────────────

func weightToBase(value float64, unit string) (float64, error) {
	switch unit {
	case "grams", "gram", "g":
		return value, nil
	case "kilograms", "kilogram", "kg":
		return value * 1000, nil
	case "pounds", "pound", "lb", "lbs":
		return value * 453.59237, nil
	case "ounces", "ounce", "oz":
		return value * 28.349523125, nil
	}
	return 0, fmt.Errorf("unknown weight unit %q", unit)
}

func weightFromBase(grams float64, unit string) (float64, error) {
	switch unit {
	case "grams", "gram", "g":
		return grams, nil
	case "kilograms", "kilogram", "kg":
		return grams / 1000, nil
	case "pounds", "pound", "lb", "lbs":
		return grams / 453.59237, nil
	case "ounces", "ounce", "oz":
		return grams / 28.349523125, nil
	}
	return 0, fmt.Errorf("unknown weight unit %q", unit)
}

// ── Speed (base unit: km/h) ────────────────────────────────────────────────────

func speedToBase(value float64, unit string) (float64, error) {
	switch unit {
	case "km/h", "kph", "kmh", "kilometers per hour":
		return value, nil
	case "miles/h", "mph", "miles per hour":
		return value * 1.60934, nil
	case "m/s", "meters per second":
		return value * 3.6, nil
	}
	return 0, fmt.Errorf("unknown speed unit %q", unit)
}

func speedFromBase(kmh float64, unit string) (float64, error) {
	switch unit {
	case "km/h", "kph", "kmh", "kilometers per hour":
		return kmh, nil
	case "miles/h", "mph", "miles per hour":
		return kmh / 1.60934, nil
	case "m/s", "meters per second":
		return kmh / 3.6, nil
	}
	return 0, fmt.Errorf("unknown speed unit %q", unit)
}
