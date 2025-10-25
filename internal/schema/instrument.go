package schema

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/coachpo/meltica/internal/errs"
)

const (
	isoDateLayout      = "2006-01-02"
	symbolDateLayout   = "20060102"
	perpetualSuffix    = "PERP"
	maxSymbolSegments  = 6
	minSymbolSegments  = 2
	optionsSegmentsMin = 4
	optionsSegmentsMax = 5
	maxPrecisionDigits = 18
)

// Instrument describes a tradable instrument across supported venues.
type Instrument struct {
	Symbol            string         `json:"symbol"`
	Type              InstrumentType `json:"type"`
	BaseCurrency      string         `json:"base_currency"`
	QuoteCurrency     string         `json:"quote_currency"`
	Venue             string         `json:"venue"`
	Expiry            string         `json:"expiry,omitempty"`
	ContractValue     *float64       `json:"contract_value,omitempty"`
	ContractCurrency  string         `json:"contract_currency,omitempty"`
	Strike            *float64       `json:"strike,omitempty"`
	OptionType        OptionType     `json:"option_type,omitempty"`
	PriceIncrement    string         `json:"price_increment,omitempty"`
	QuantityIncrement string         `json:"quantity_increment,omitempty"`
	PricePrecision    *int           `json:"price_precision,omitempty"`
	QuantityPrecision *int           `json:"quantity_precision,omitempty"`
	NotionalPrecision *int           `json:"notional_precision,omitempty"`
	MinNotional       string         `json:"min_notional,omitempty"`
	MinQuantity       string         `json:"min_quantity,omitempty"`
	MaxQuantity       string         `json:"max_quantity,omitempty"`
}

// InstrumentType identifies the market structure for an instrument.
type InstrumentType string

const (
	// InstrumentTypeSpot represents spot markets.
	InstrumentTypeSpot InstrumentType = "spot"
	// InstrumentTypePerp represents perpetual swap markets.
	InstrumentTypePerp InstrumentType = "perp"
	// InstrumentTypeFutures represents dated futures markets.
	InstrumentTypeFutures InstrumentType = "futures"
	// InstrumentTypeOptions represents options markets.
	InstrumentTypeOptions InstrumentType = "options"
)

// Valid reports whether the instrument type is recognised.
func (it InstrumentType) Valid() bool {
	switch it {
	case InstrumentTypeSpot,
		InstrumentTypePerp,
		InstrumentTypeFutures,
		InstrumentTypeOptions:
		return true
	default:
		return false
	}
}

// OptionType identifies option style for options instruments.
type OptionType string

const (
	// OptionTypeCall represents call options.
	OptionTypeCall OptionType = "call"
	// OptionTypePut represents put options.
	OptionTypePut OptionType = "put"
)

// Valid reports whether the option type is recognised.
func (ot OptionType) Valid() bool {
	switch ot {
	case OptionTypeCall, OptionTypePut:
		return true
	default:
		return false
	}
}

type symbolMeta struct {
	expirySegment string
	strikeSegment string
	optionMarker  OptionType
}

func normalizeRequiredCurrency(code, field string) (string, error) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return "", instrumentError(field + " required")
	}
	if len(trimmed) < 2 || len(trimmed) > 10 {
		return "", instrumentError(field + " must be 2-10 characters")
	}
	if !isUpperAlnum(trimmed) {
		return "", instrumentError(field + " must contain only uppercase letters and digits")
	}
	return trimmed, nil
}

func normalizeOptionalCurrency(code, field string) (string, error) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) < 2 || len(trimmed) > 10 {
		return "", instrumentError(field + " must be 2-10 characters")
	}
	if !isUpperAlnum(trimmed) {
		return "", instrumentError(field + " must contain only uppercase letters and digits")
	}
	return trimmed, nil
}

func normalizeVenue(code string) (string, error) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return "", instrumentError("instrument.venue required")
	}
	if len(trimmed) < 2 || len(trimmed) > 20 {
		return "", instrumentError("instrument.venue must be 2-20 characters")
	}
	for _, r := range trimmed {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return "", instrumentError("instrument.venue must contain only uppercase letters, digits, or underscore")
		}
	}
	return trimmed, nil
}

// Validate ensures the instrument payload adheres to the canonical schema.
func (i *Instrument) Validate() error {
	if i == nil {
		return instrumentError("instrument payload required")
	}

	typ := InstrumentType(strings.ToLower(strings.TrimSpace(string(i.Type))))
	if typ == "" {
		return instrumentError("instrument.type required")
	}
	if !typ.Valid() {
		return instrumentError("instrument.type invalid")
	}
	i.Type = typ

	symbol := strings.TrimSpace(i.Symbol)
	if symbol == "" {
		return instrumentError("instrument.symbol required")
	}
	parts, err := validateInstrumentSymbol(symbol)
	if err != nil {
		return err
	}
	meta, err := validateSymbolForType(parts, typ)
	if err != nil {
		return err
	}
	i.Symbol = symbol

	base, err := normalizeRequiredCurrency(i.BaseCurrency, "instrument.base_currency")
	if err != nil {
		return err
	}
	i.BaseCurrency = base

	quote, err := normalizeRequiredCurrency(i.QuoteCurrency, "instrument.quote_currency")
	if err != nil {
		return err
	}
	i.QuoteCurrency = quote

	if parts[0] != i.BaseCurrency {
		return instrumentError("instrument.base_currency must match symbol base segment")
	}
	if parts[1] != i.QuoteCurrency {
		return instrumentError("instrument.quote_currency must match symbol quote segment")
	}

	venue, err := normalizeVenue(i.Venue)
	if err != nil {
		return err
	}
	i.Venue = venue

	if err := validateExpiry(i, meta); err != nil {
		return err
	}
	if err := validateContractNotional(i); err != nil {
		return err
	}
	if err := validateOptionsSpecifics(i, meta); err != nil {
		return err
	}
	if err := validateTradingConstraints(i); err != nil {
		return err
	}
	return nil
}

func validateInstrumentSymbol(symbol string) ([]string, error) {
	if !strings.Contains(symbol, "-") {
		return nil, instrumentError("instrument.symbol must contain '-' separators")
	}
	parts := strings.Split(symbol, "-")
	if len(parts) < minSymbolSegments {
		return nil, instrumentError("instrument.symbol must contain at least base and quote segments")
	}
	if len(parts) > maxSymbolSegments {
		return nil, instrumentError("instrument.symbol contains too many segments")
	}
	for _, part := range parts {
		if part == "" {
			return nil, instrumentError("instrument.symbol contains empty segment")
		}
		if strings.ToUpper(part) != part {
			return nil, instrumentError("instrument.symbol segments must be uppercase")
		}
	}
	return parts, nil
}

func validateSymbolForType(parts []string, typ InstrumentType) (symbolMeta, error) {
	meta := symbolMeta{
		expirySegment: "",
		strikeSegment: "",
		optionMarker:  OptionType(""),
	}
	if len(parts) >= 2 {
		if !isCurrencyCode(parts[0]) {
			return meta, instrumentError("instrument.symbol base segment must be 2-10 uppercase alphanumeric characters")
		}
		if !isCurrencyCode(parts[1]) {
			return meta, instrumentError("instrument.symbol quote segment must be 2-10 uppercase alphanumeric characters")
		}
	}
	switch typ {
	case InstrumentTypeSpot:
		if len(parts) != 2 {
			return meta, instrumentError("spot instrument symbol must follow BASE-QUOTE")
		}
	case InstrumentTypePerp:
		if len(parts) != 3 {
			return meta, instrumentError("perp instrument symbol must follow BASE-QUOTE-PERP")
		}
		if parts[2] != perpetualSuffix {
			return meta, instrumentError("perp instrument symbol must end with '-PERP'")
		}
	case InstrumentTypeFutures:
		if len(parts) != 3 {
			return meta, instrumentError("futures instrument symbol must follow BASE-QUOTE-YYYYMMDD")
		}
		if !isValidSymbolDate(parts[2]) {
			return meta, instrumentError("futures instrument symbol requires an 8 digit expiry segment (YYYYMMDD)")
		}
		meta.expirySegment = parts[2]
	case InstrumentTypeOptions:
		if len(parts) < optionsSegmentsMin || len(parts) > optionsSegmentsMax {
			return meta, instrumentError("options instrument symbol must follow BASE-QUOTE-YYYYMMDD-STRIKE[-C|P]")
		}
		if !isValidSymbolDate(parts[2]) {
			return meta, instrumentError("options instrument symbol requires an 8 digit expiry segment (YYYYMMDD)")
		}
		if _, err := strconv.ParseFloat(parts[3], 64); err != nil {
			return meta, instrumentError("options instrument symbol strike segment must be numeric")
		}
		meta.expirySegment = parts[2]
		meta.strikeSegment = parts[3]
		if len(parts) == optionsSegmentsMax {
			optionMarker, ok := optionMarkerFromSymbol(parts[4])
			if !ok {
				return meta, instrumentError("options instrument symbol has an invalid option marker")
			}
			meta.optionMarker = optionMarker
		}
	default:
		if len(parts) < minSymbolSegments {
			return meta, instrumentError("instrument symbol must contain base and quote segments")
		}
	}
	return meta, nil
}

func validateExpiry(i *Instrument, meta symbolMeta) error {
	expiry := strings.TrimSpace(i.Expiry)
	switch i.Type {
	case InstrumentTypeFutures, InstrumentTypeOptions:
		if expiry == "" {
			return instrumentError("instrument.expiry required for futures/options instruments")
		}
		parsed, err := time.Parse(isoDateLayout, expiry)
		if err != nil {
			return instrumentError("instrument.expiry must be ISO-8601 date (YYYY-MM-DD)")
		}
		if meta.expirySegment != "" && parsed.Format(symbolDateLayout) != meta.expirySegment {
			return instrumentError("instrument.expiry does not match symbol expiry segment")
		}
		i.Expiry = parsed.Format(isoDateLayout)
	case InstrumentTypeSpot, InstrumentTypePerp:
		if expiry != "" {
			return instrumentError("instrument.expiry must be omitted for spot/perp instruments")
		}
		i.Expiry = ""
	}
	return nil
}

func validateContractNotional(i *Instrument) error {
	switch i.Type {
	case InstrumentTypeSpot:
		if i.ContractValue != nil {
			return instrumentError("instrument.contract_value must be omitted for spot instruments")
		}
		if strings.TrimSpace(i.ContractCurrency) != "" {
			return instrumentError("instrument.contract_currency must be omitted for spot instruments")
		}
		i.ContractCurrency = ""
	case InstrumentTypePerp, InstrumentTypeFutures, InstrumentTypeOptions:
		normalized, err := normalizeOptionalCurrency(i.ContractCurrency, "instrument.contract_currency")
		if err != nil {
			return err
		}
		if normalized != "" && i.ContractValue == nil {
			return instrumentError("instrument.contract_value required when contract_currency is provided")
		}
		if i.ContractValue != nil && normalized == "" {
			return instrumentError("instrument.contract_currency required when contract_value is provided")
		}
		if i.ContractValue != nil && *i.ContractValue <= 0 {
			return instrumentError("instrument.contract_value must be greater than zero when provided")
		}
		i.ContractCurrency = normalized
	default:
		return instrumentError("instrument.type invalid")
	}
	return nil
}

func validateOptionsSpecifics(i *Instrument, meta symbolMeta) error {
	if i.Type != InstrumentTypeOptions {
		if i.Strike != nil {
			return instrumentError("instrument.strike must be omitted for non-options instruments")
		}
		if strings.TrimSpace(string(i.OptionType)) != "" {
			return instrumentError("instrument.option_type must be omitted for non-options instruments")
		}
		i.OptionType = ""
		return nil
	}

	if i.Strike == nil {
		return instrumentError("instrument.strike required for options instruments")
	}
	if *i.Strike <= 0 {
		return instrumentError("instrument.strike must be greater than zero for options instruments")
	}
	if meta.strikeSegment != "" {
		if strikeValue, err := strconv.ParseFloat(meta.strikeSegment, 64); err == nil {
			if math.Abs(strikeValue-*i.Strike) > 1e-9 {
				return instrumentError("instrument.strike does not match symbol strike segment")
			}
		}
	}

	optionType := OptionType(strings.ToLower(strings.TrimSpace(string(i.OptionType))))
	if optionType == "" {
		return instrumentError("instrument.option_type required for options instruments")
	}
	if !optionType.Valid() {
		return instrumentError("instrument.option_type invalid")
	}
	if meta.optionMarker != "" && meta.optionMarker != optionType {
		return instrumentError("instrument.option_type does not match symbol option marker")
	}
	i.OptionType = optionType
	return nil
}

func validateTradingConstraints(i *Instrument) error {
	priceIncrement, _, err := normalizeOptionalDecimal(i.PriceIncrement, "instrument.price_increment", true)
	if err != nil {
		return err
	}
	i.PriceIncrement = priceIncrement

	quantityIncrement, _, err := normalizeOptionalDecimal(i.QuantityIncrement, "instrument.quantity_increment", true)
	if err != nil {
		return err
	}
	i.QuantityIncrement = quantityIncrement

	if err := validatePrecisionField(i.PricePrecision, "instrument.price_precision"); err != nil {
		return err
	}
	if err := validatePrecisionField(i.QuantityPrecision, "instrument.quantity_precision"); err != nil {
		return err
	}
	if err := validatePrecisionField(i.NotionalPrecision, "instrument.notional_precision"); err != nil {
		return err
	}

	minQuantity, minQuantityRat, err := normalizeOptionalDecimal(i.MinQuantity, "instrument.min_quantity", true)
	if err != nil {
		return err
	}
	i.MinQuantity = minQuantity

	maxQuantity, maxQuantityRat, err := normalizeOptionalDecimal(i.MaxQuantity, "instrument.max_quantity", true)
	if err != nil {
		return err
	}
	i.MaxQuantity = maxQuantity

	if minQuantityRat != nil && maxQuantityRat != nil && maxQuantityRat.Cmp(minQuantityRat) < 0 {
		return instrumentError("instrument.max_quantity must be greater than or equal to instrument.min_quantity")
	}

	minNotional, _, err := normalizeOptionalDecimal(i.MinNotional, "instrument.min_notional", true)
	if err != nil {
		return err
	}
	i.MinNotional = minNotional

	return nil
}

func normalizeOptionalDecimal(value, field string, strictlyPositive bool) (string, *big.Rat, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil, nil
	}
	rational, ok := new(big.Rat).SetString(trimmed)
	if !ok {
		return "", nil, instrumentError(field + " must be a decimal string")
	}
	if strictlyPositive {
		if rational.Sign() <= 0 {
			return "", nil, instrumentError(field + " must be greater than zero")
		}
	} else if rational.Sign() < 0 {
		return "", nil, instrumentError(field + " must be zero or greater")
	}
	return trimmed, rational, nil
}

func validatePrecisionField(value *int, field string) error {
	if value == nil {
		return nil
	}
	precision := *value
	if precision < 0 {
		return instrumentError(field + " must be greater than or equal to zero")
	}
	if precision > maxPrecisionDigits {
		return instrumentError(field + " must be less than or equal to 18")
	}
	return nil
}

func isValidSymbolDate(segment string) bool {
	if len(segment) != len(symbolDateLayout) {
		return false
	}
	_, err := time.Parse(symbolDateLayout, segment)
	return err == nil
}

func optionMarkerFromSymbol(segment string) (OptionType, bool) {
	switch segment {
	case "C", "CALL":
		return OptionTypeCall, true
	case "P", "PUT":
		return OptionTypePut, true
	default:
		return "", false
	}
}

func instrumentError(msg string) error {
	return errs.New("schema/instrument", errs.CodeInvalid, errs.WithMessage(msg))
}

func isUpperAlnum(value string) bool {
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func isCurrencyCode(segment string) bool {
	length := len(segment)
	if length < 2 || length > 10 {
		return false
	}
	return isUpperAlnum(segment)
}

// InstrumentCurrencies extracts the base and quote currency codes from a canonical instrument symbol.
func InstrumentCurrencies(symbol string) (string, string, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return "", "", instrumentError("instrument.symbol required")
	}
	parts, err := validateInstrumentSymbol(symbol)
	if err != nil {
		return "", "", err
	}
	if len(parts) < 2 {
		return "", "", instrumentError("instrument symbol must contain base and quote segments")
	}
	return parts[0], parts[1], nil
}

// NormalizeCurrencyCode normalizes a currency identifier to uppercase and validates its format.
func NormalizeCurrencyCode(code string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(code))
	if trimmed == "" {
		return ""
	}
	if !isCurrencyCode(trimmed) {
		return ""
	}
	return trimmed
}

// CloneInstrument creates a deep copy of the provided instrument.
func CloneInstrument(inst Instrument) Instrument {
	clone := inst
	if inst.ContractValue != nil {
		v := *inst.ContractValue
		clone.ContractValue = &v
	}
	if inst.Strike != nil {
		v := *inst.Strike
		clone.Strike = &v
	}
	if inst.PricePrecision != nil {
		v := *inst.PricePrecision
		clone.PricePrecision = &v
	}
	if inst.QuantityPrecision != nil {
		v := *inst.QuantityPrecision
		clone.QuantityPrecision = &v
	}
	if inst.NotionalPrecision != nil {
		v := *inst.NotionalPrecision
		clone.NotionalPrecision = &v
	}
	return clone
}

// CloneInstruments returns deep copies of the provided instruments slice.
func CloneInstruments(list []Instrument) []Instrument {
	if len(list) == 0 {
		return nil
	}
	out := make([]Instrument, len(list))
	for i := range list {
		out[i] = CloneInstrument(list[i])
	}
	return out
}
