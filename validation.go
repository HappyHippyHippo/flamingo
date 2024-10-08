package flam

import (
	"errors"
	"net/http"
	"reflect"
	"strconv"

	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	trans "github.com/go-playground/validator/v10/translations/en"
)

var (
	// ValidationLocale @todo doc
	ValidationLocale = EnvString("FLAMINGO_VALIDATION_LOCALE", "en")
)

func newValidationUniversalTranslator() *ut.UniversalTranslator {
	lang := en.New()
	return ut.New(lang, lang)
}

func newValidationTranslator(universalTranslator *ut.UniversalTranslator) (ut.Translator, error) {
	translator, found := universalTranslator.GetTranslator(ValidationLocale)
	if found == false {
		return nil, NewError("translator not found", Bag{"locale": ValidationLocale})
	}
	return translator, nil
}

// ValidationParser @todo doc
type ValidationParser interface {
	AddError(err string, code int)
	Parse(value interface{}, errs validator.ValidationErrors) Envelope
}

type validationParser struct {
	mapper     map[string]int
	translator ut.Translator
}

var _ ValidationParser = &validationParser{}

func newValidationParser(translator ut.Translator) *validationParser {
	return &validationParser{
		mapper: map[string]int{
			"eqcsfield":     1,
			"eqfield":       2,
			"fieldcontains": 3,
			"fieldexcludes": 4,
			"gtcsfield":     5,
			"gtecsfield":    6,
			"gtefield":      7,
			"gtfield":       8,
			"ltcsfield":     9,
			"ltecsfield":    10,
			"ltefield":      11,
			"ltfield":       12,
			"necsfield":     13,
			"nefield":       14,

			"cidr":             15,
			"cidrv4":           16,
			"cidrv6":           17,
			"datauri":          18,
			"fqdn":             19,
			"hostname":         20,
			"hostname_port":    21,
			"hostname_rfc1123": 22,
			"ip":               23,
			"ip4_addr":         24,
			"ip6_addr":         25,
			"ip_addr":          26,
			"ipv4":             27,
			"ipv6":             28,
			"mac":              29,
			"tcp4_addr":        30,
			"tcp6_addr":        31,
			"tcp_addr":         32,
			"udp4_addr":        33,
			"udp6_addr":        34,
			"udp_addr":         35,
			"unix_addr":        36,
			"uri":              37,
			"url":              38,
			"url_encoded":      39,
			"urn_rfc2141":      40,

			"alpha":           41,
			"alphanum":        42,
			"alphanumunicode": 43,
			"alphaunicode":    44,
			"ascii":           45,
			"contains":        46,
			"containsany":     47,
			"containsrune":    48,
			"endswith":        49,
			"lowercase":       50,
			"multibyte":       51,
			"number":          52,
			"numeric":         53,
			"printascii":      54,
			"startswith":      55,
			"uppercase":       56,

			"base64":          57,
			"base64url":       58,
			"btc_addr":        59,
			"btc_addr_bech32": 60,
			"datetime":        61,
			"e164":            62,
			"email":           63,
			"eth_addr":        64,
			"hexadecimal":     65,
			"hexcolor":        66,
			"hsl":             67,
			"hsla":            68,
			"html":            69,
			"html_encoded":    70,
			"isbn":            71,
			"isbn10":          72,
			"isbn13":          73,
			"json":            74,
			"latitude":        75,
			"longitude":       76,
			"rgb":             77,
			"rgba":            78,
			"ssn":             79,
			"uuid":            80,
			"uuid3":           81,
			"uuid3_rfc4122":   82,
			"uuid4":           83,
			"uuid4_rfc4122":   84,
			"uuid5":           85,
			"uuid5_rfc4122":   86,
			"uuid_rfc4122":    87,

			"eq":  88,
			"gt":  89,
			"gte": 90,
			"lt":  91,
			"lte": 92,
			"ne":  93,

			"dir":                  94,
			"excludes":             95,
			"excludesall":          96,
			"excludesrune":         97,
			"file":                 98,
			"isdefault":            99,
			"len":                  100,
			"max":                  101,
			"min":                  102,
			"oneof":                103,
			"required":             104,
			"required_if":          105,
			"required_unless":      106,
			"required_with":        107,
			"required_with_all":    108,
			"required_without":     109,
			"required_without_all": 110,
			"excluded_with":        111,
			"excluded_with_all":    112,
			"excluded_without":     113,
			"excluded_without_all": 114,
			"unique":               115,
		},
		translator: translator,
	}
}

func (p *validationParser) AddError(e string, code int) {
	p.mapper[e] = code
}

func (p *validationParser) Parse(value interface{}, ve validator.ValidationErrors) Envelope {
	if len(ve) == 0 {
		return nil
	}
	env := NewEnvelope()
	for _, e := range ve {
		paramID, errorID, e := p.convert(value, e)
		env.AddError(http.StatusBadRequest, e).SetParamID(paramID).SetErrorID(errorID)
	}
	return env
}

func (p *validationParser) convert(value interface{}, fe validator.FieldError) (int, int, error) {
	typeof := reflect.TypeOf(value)
	field, _ := typeof.FieldByName(fe.StructField())
	iParam := 0
	if param, ok := field.Tag.Lookup("vparam"); ok {
		iParam, _ = strconv.Atoi(param)
	}
	return iParam, p.mapper[fe.Tag()], errors.New(fe.Translate(p.translator))
}

func newBaseValidator(translator ut.Translator) (*validator.Validate, error) {
	v := validator.New()
	if e := trans.RegisterDefaultTranslations(v, translator); e != nil {
		return nil, e
	}
	return v, nil
}

// Validator @todo doc
type Validator func(val interface{}) Envelope

func newValidator(validate *validator.Validate, parser *validationParser) Validator {
	return func(value interface{}) Envelope {
		if errs := validate.Struct(value); errs != nil {
			return parser.Parse(value, errs.(validator.ValidationErrors))
		}
		return nil
	}
}

type validationProvider struct{}

var _ Provider = &validationProvider{}

func newValidationProvider() Provider {
	return &validationProvider{}
}

func (*validationProvider) ID() string {
	return "flam.validation"
}

func (p *validationProvider) Reg(app App) error {
	_ = app.DI().Provide(newValidationUniversalTranslator)
	_ = app.DI().Provide(newValidationTranslator)
	_ = app.DI().Provide(newValidationParser)
	_ = app.DI().Provide(newBaseValidator)
	_ = app.DI().Provide(newValidator)
	return nil
}
