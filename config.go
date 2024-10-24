package flam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/afero"
	"go.uber.org/dig"
	"gopkg.in/yaml.v3"
)

const (
	// ConfigParserTag @todo doc
	ConfigParserTag = "flam.config.parser"

	// ConfigSourceTag @todo doc
	ConfigSourceTag = "flam.config.source"

	// ConfigAggregateTag @todo doc
	ConfigAggregateTag = "flam.config.aggregate"
)

var (
	// ConfigLoaderActive @todo doc
	ConfigLoaderActive = EnvBool("FLAMINGO_CONFIG_LOADER_ACTIVE", true)

	// ConfigLoaderFilePath @todo doc
	ConfigLoaderFilePath = EnvString("FLAMINGO_CONFIG_LOADER_FILE_PATH", "config/config.yaml")

	// ConfigLoaderFileFormat @todo doc
	ConfigLoaderFileFormat = EnvString("FLAMINGO_CONFIG_LOADER_FILE_FORMAT", "yaml")

	// ConfigLoaderSourceID @todo doc
	ConfigLoaderSourceID = EnvString("FLAMINGO_CONFIG_LOADER_SOURCE_ID", "_sources")

	// ConfigLoaderConfigPath @todo doc
	ConfigLoaderConfigPath = EnvString("FLAMINGO_CONFIG_LOADER_CONFIG_PATH", "flam.config")

	// ConfigObserveFrequency @todo doc
	ConfigObserveFrequency = EnvInt("FLAMINGO_CONFIG_OBSERVE_FREQUENCY", 0)
)

// Config @todo doc
type Config interface {
	Clone() *Bag
	Entries() []string
	Has(path string) bool
	Set(path string, value interface{}) error
	Get(path string, def interface{}) interface{}
	Bool(path string, def bool) bool
	Int(path string, def int) int
	Float(path string, def float64) float64
	String(path, def string) string
	List(path string, def []interface{}) []interface{}
	Bag(path string, def *Bag) *Bag
	Merge(src Bag) *Bag
	Populate(path string, target interface{}, insensitive ...bool) error
}

// ConfigParser @todo doc
type ConfigParser interface {
	Parse() (*Bag, error)
}

// ConfigParserCreator @todo doc
type ConfigParserCreator interface {
	Accept(config *Bag) bool
	Create(config *Bag) (ConfigParser, error)
}

// ConfigParserFactory @todo doc
type ConfigParserFactory interface {
	AddCreator(creator ConfigParserCreator) ConfigParserFactory
	Create(config *Bag) (ConfigParser, error)
}

type configParserFactory []ConfigParserCreator

var _ ConfigParserFactory = &configParserFactory{}

func newConfigParserFactory(
	args struct {
		dig.In
		Creators []ConfigParserCreator `group:"flam.config.parser"`
	},
) ConfigParserFactory {
	factory := &configParserFactory{}
	for _, creator := range args.Creators {
		*factory = append(*factory, creator)
	}
	return factory
}

func (f *configParserFactory) AddCreator(creator ConfigParserCreator) ConfigParserFactory {
	*f = append(*f, creator)
	return f
}

func (f *configParserFactory) Create(config *Bag) (ConfigParser, error) {
	for _, creator := range *f {
		if creator.Accept(config) {
			return creator.Create(config)
		}
	}
	return nil, NewError("unable to parse validationParser config", Bag{"config": config})
}

type configDecoder interface {
	Decode(config interface{}) error
}

type configParser struct {
	reader  io.Reader
	decoder configDecoder
}

var _ ConfigParser = &configParser{}

func (d *configParser) Close() error {
	if d.reader != nil {
		if closer, ok := d.reader.(io.Closer); ok {
			if e := closer.Close(); e != nil {
				return e
			}
		}
		d.reader = nil
	}
	return nil
}

func (d *configParser) Parse() (*Bag, error) {
	return nil, nil
}

func (d *configParser) convert(val interface{}) interface{} {
	if pValue, ok := val.(Bag); ok {
		p := Bag{}
		for k, value := range pValue {
			p[strings.ToLower(k)] = d.convert(value)
		}
		return p
	}
	if lValue, ok := val.([]interface{}); ok {
		var result []interface{}
		for _, i := range lValue {
			result = append(result, d.convert(i))
		}
		return result
	}
	if mValue, ok := val.(map[string]interface{}); ok {
		result := Bag{}
		for k, i := range mValue {
			result[strings.ToLower(k)] = d.convert(i)
		}
		return result
	}
	if mValue, ok := val.(map[interface{}]interface{}); ok {
		result := Bag{}
		for k, i := range mValue {
			stringKey, ok := k.(string)
			if ok {
				result[strings.ToLower(stringKey)] = d.convert(i)
			} else {
				result[fmt.Sprintf("%v", k)] = d.convert(i)
			}
		}
		return result
	}
	if fValue, ok := val.(float64); ok && float64(int(fValue)) == fValue {
		return int(fValue)
	}
	if fValue, ok := val.(float32); ok && float32(int(fValue)) == fValue {
		return int(fValue)
	}
	return val
}

type jsonConfigParser struct {
	configParser
}

var _ ConfigParser = &jsonConfigParser{}

func (d jsonConfigParser) Parse() (*Bag, error) {
	data := map[string]interface{}{}
	if e := d.decoder.Decode(&data); e != nil {
		return nil, e
	}
	result := d.convert(data).(Bag)
	return &result, nil
}

type jsonConfigParserCreator struct {
	config struct {
		Format string
		Reader io.Reader
	}
}

var _ ConfigParserCreator = &jsonConfigParserCreator{}

func (pc *jsonConfigParserCreator) Accept(config *Bag) bool {
	pc.config.Format = ""
	pc.config.Reader = nil
	if e := config.Populate("", &pc.config); e != nil {
		return false
	}
	return strings.ToLower(pc.config.Format) == "json" && pc.config.Reader != nil
}

func (pc *jsonConfigParserCreator) Create(_ *Bag) (ConfigParser, error) {
	return &jsonConfigParser{
		configParser: configParser{
			reader:  pc.config.Reader,
			decoder: json.NewDecoder(pc.config.Reader),
		},
	}, nil
}

type yamlConfigParser struct {
	configParser
}

var _ ConfigParser = &yamlConfigParser{}

func (d yamlConfigParser) Parse() (*Bag, error) {
	data := Bag{}
	if e := d.decoder.Decode(&data); e != nil {
		return nil, e
	}
	result := d.convert(data).(Bag)
	return &result, nil
}

type yamlConfigParserCreator struct {
	config struct {
		Format string
		Reader io.Reader
	}
}

var _ ConfigParserCreator = &yamlConfigParserCreator{}

func (pc *yamlConfigParserCreator) Accept(config *Bag) bool {
	pc.config.Format = ""
	pc.config.Reader = nil
	if e := config.Populate("", &pc.config); e != nil {
		return false
	}
	return strings.ToLower(pc.config.Format) == "yaml" && pc.config.Reader != nil
}

func (pc *yamlConfigParserCreator) Create(_ *Bag) (ConfigParser, error) {
	return &yamlConfigParser{
		configParser: configParser{
			reader:  pc.config.Reader,
			decoder: yaml.NewDecoder(pc.config.Reader),
		},
	}, nil
}

// ConfigSource @todo doc
type ConfigSource interface {
	Has(path string) bool
	Get(path string, def interface{}) interface{}
}

// ConfigObsSource @todo doc
type ConfigObsSource interface {
	ConfigSource
	Reload() (bool, error)
}

// ConfigSourceCreator @todo doc
type ConfigSourceCreator interface {
	Accept(config *Bag) bool
	Create(config *Bag) (ConfigSource, error)
}

// ConfigSourceFactory @todo doc
type ConfigSourceFactory interface {
	AddCreator(creator ConfigSourceCreator) ConfigSourceFactory
	Create(config *Bag) (ConfigSource, error)
}

type configSource struct {
	mutex   sync.Locker
	partial Bag
}

var _ ConfigSource = &configSource{}

func (s *configSource) Has(path string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.partial.Has(path)
}

func (s *configSource) Get(path string, def interface{}) interface{} {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.partial.Get(path, def)
}

type configSourceFactory []ConfigSourceCreator

var _ ConfigSourceFactory = &configSourceFactory{}

func newConfigSourceFactory(
	args struct {
		dig.In
		Creators []ConfigSourceCreator `group:"flam.config.source"`
	},
) ConfigSourceFactory {
	factory := &configSourceFactory{}
	for _, creator := range args.Creators {
		*factory = append(*factory, creator)
	}
	return factory
}

func (f *configSourceFactory) AddCreator(creator ConfigSourceCreator) ConfigSourceFactory {
	*f = append(*f, creator)
	return f
}

func (f *configSourceFactory) Create(config *Bag) (ConfigSource, error) {
	for _, creator := range *f {
		if creator.Accept(config) {
			return creator.Create(config)
		}
	}
	return nil, NewError("unable to parse source config", Bag{"config": config})
}

type aggregateConfigSource struct {
	configSource
	sources []ConfigSource
}

var _ ConfigSource = &aggregateConfigSource{}

func (c *aggregateConfigSource) load() {
	partial := Bag{}
	for _, s := range c.sources {
		p := s.Get("", Bag{}).(Bag)
		partial.Merge(p)
	}
	c.mutex.Lock()
	c.partial = partial
	c.mutex.Unlock()
}

type aggregateConfigSourceCreator struct {
	sources []ConfigSource
	config  struct {
		Type string
	}
}

var _ ConfigSourceCreator = &aggregateConfigSourceCreator{}

func newAggregateConfigSourceCreator(
	args struct {
		dig.In
		Sources []ConfigSource `group:"flam.config.aggregate"`
	},
) ConfigSourceCreator {
	return &aggregateConfigSourceCreator{
		sources: args.Sources,
	}
}

func (sc *aggregateConfigSourceCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "aggregate"
}

func (sc *aggregateConfigSourceCreator) Create(_ *Bag) (ConfigSource, error) {
	source := &aggregateConfigSource{
		configSource: configSource{
			mutex:   &sync.Mutex{},
			partial: Bag{},
		},
		sources: sc.sources,
	}
	source.load()
	return source, nil
}

type envConfigSource struct {
	configSource
	mappings map[string]string
}

var _ ConfigSource = &envConfigSource{}

func (s *envConfigSource) load() {
	for key, path := range s.mappings {
		env := os.Getenv(key)
		if env == "" {
			continue
		}
		step := s.partial
		sections := strings.Split(path, ".")
		for i, section := range sections {
			if i != len(sections)-1 {
				if _, ok := step[section]; ok == false {
					step[section] = Bag{}
				}
				step[section] = Bag{}
				step = step[section].(Bag)
			} else {
				step[section] = env
			}
		}
	}
}

type envConfigSourceCreator struct {
	config struct {
		Type     string
		Mappings Bag
	}
}

var _ ConfigSourceCreator = &envConfigSourceCreator{}

func newEnvConfigSourceCreator() ConfigSourceCreator {
	return &envConfigSourceCreator{}
}

func (sc *envConfigSourceCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.Mappings = Bag{}
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "env"
}

func (sc *envConfigSourceCreator) Create(_ *Bag) (ConfigSource, error) {
	mappings := map[string]string{}
	for k, value := range sc.config.Mappings {
		typedValue, ok := value.(string)
		if !ok {
			continue
		}
		mappings[k] = typedValue
	}
	source := &envConfigSource{
		configSource: configSource{
			mutex:   &sync.Mutex{},
			partial: Bag{},
		},
		mappings: mappings,
	}
	source.load()
	return source, nil
}

type fileConfigSource struct {
	configSource
	path          string
	format        string
	fileSystem    afero.Fs
	parserFactory ConfigParserFactory
}

var _ ConfigSource = &fileConfigSource{}

func (s *fileConfigSource) load() error {
	file, e := s.fileSystem.OpenFile(s.path, os.O_RDONLY, 0o644)
	if e != nil {
		return e
	}
	parser, e := s.parserFactory.Create(&Bag{"format": s.format, "reader": file})
	if e != nil {
		_ = file.Close()
		return e
	}
	defer func() {
		if closer, ok := parser.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	partial, e := parser.Parse()
	if e != nil {
		return e
	}
	s.mutex.Lock()
	s.partial = *partial
	s.mutex.Unlock()
	return nil
}

type fileConfigSourceCreator struct {
	fileSystem    afero.Fs
	parserFactory ConfigParserFactory
	config        struct {
		Type   string
		Path   string
		Format string
	}
}

var _ ConfigSourceCreator = &fileConfigSourceCreator{}

func newFileConfigSourceCreator(fileSystem afero.Fs, parserFactory ConfigParserFactory) ConfigSourceCreator {
	return &fileConfigSourceCreator{
		fileSystem:    fileSystem,
		parserFactory: parserFactory,
	}
}

func (sc *fileConfigSourceCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.Path = ""
	sc.config.Format = "yaml"
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "file" &&
		sc.config.Path != "" &&
		sc.config.Format != ""
}

func (sc *fileConfigSourceCreator) Create(_ *Bag) (ConfigSource, error) {
	source := &fileConfigSource{
		configSource: configSource{
			mutex:   &sync.Mutex{},
			partial: Bag{},
		},
		path:          sc.config.Path,
		format:        sc.config.Format,
		fileSystem:    sc.fileSystem,
		parserFactory: sc.parserFactory,
	}
	if e := source.load(); e != nil {
		return nil, e
	}
	return source, nil
}

type obsFileConfigSource struct {
	fileConfigSource
	timestamp time.Time
}

var _ ConfigSource = &obsFileConfigSource{}
var _ ConfigObsSource = &obsFileConfigSource{}

func (s *obsFileConfigSource) Reload() (bool, error) {
	fileStats, e := s.fileSystem.Stat(s.path)
	if e != nil {
		return false, e
	}
	modTime := fileStats.ModTime()
	if s.timestamp.Equal(time.Unix(0, 0)) || s.timestamp.Before(modTime) {
		if e := s.load(); e != nil {
			return false, e
		}
		s.timestamp = modTime
		return true, nil
	}
	return false, nil
}

type obsFileConfigSourceCreator struct {
	fileConfigSourceCreator
}

var _ ConfigSourceCreator = &obsFileConfigSourceCreator{}

func newObsFileConfigSourceCreator(fileSystem afero.Fs, parserFactory ConfigParserFactory) ConfigSourceCreator {
	return &obsFileConfigSourceCreator{
		fileConfigSourceCreator: fileConfigSourceCreator{
			fileSystem:    fileSystem,
			parserFactory: parserFactory,
		},
	}
}

func (sc *obsFileConfigSourceCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.Path = ""
	sc.config.Format = "yaml"
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "obs-file" &&
		sc.config.Path != "" &&
		sc.config.Format != ""
}

func (sc *obsFileConfigSourceCreator) Create(_ *Bag) (ConfigSource, error) {
	source := &obsFileConfigSource{
		fileConfigSource: fileConfigSource{
			configSource: configSource{
				mutex:   &sync.Mutex{},
				partial: Bag{},
			},
			path:          sc.config.Path,
			format:        sc.config.Format,
			fileSystem:    sc.fileSystem,
			parserFactory: sc.parserFactory,
		},
		timestamp: time.Unix(0, 0),
	}
	if _, e := source.Reload(); e != nil {
		return nil, e
	}
	return source, nil
}

type dirConfigSource struct {
	configSource
	path          string
	format        string
	recursive     bool
	fileSystem    afero.Fs
	parserFactory ConfigParserFactory
}

var _ ConfigSource = &dirConfigSource{}

func (s *dirConfigSource) load() error {
	partial, e := s.loadDir(s.path)
	if e != nil {
		return e
	}
	s.mutex.Lock()
	s.partial = *partial
	s.mutex.Unlock()
	return nil
}

func (s *dirConfigSource) loadDir(path string) (*Bag, error) {
	dir, e := s.fileSystem.Open(path)
	if e != nil {
		return nil, e
	}
	defer func() { _ = dir.Close() }()
	files, e := dir.Readdir(0)
	if e != nil {
		return nil, e
	}
	loaded := &Bag{}
	for _, file := range files {
		if file.IsDir() {
			if s.recursive {
				partial, e := s.loadDir(path + "/" + file.Name())
				if e != nil {
					return nil, e
				}
				loaded.Merge(*partial)
			}
		} else {
			partial, e := s.loadFile(path + "/" + file.Name())
			if e != nil {
				return nil, e
			}
			loaded.Merge(*partial)
		}
	}
	return loaded, nil
}

func (s *dirConfigSource) loadFile(path string) (*Bag, error) {
	file, e := s.fileSystem.OpenFile(path, os.O_RDONLY, 0o644)
	if e != nil {
		return nil, e
	}
	parser, e := s.parserFactory.Create(&Bag{"format": s.format, "reader": file})
	if e != nil {
		_ = file.Close()
		return nil, e
	}
	defer func() {
		if closer, ok := parser.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	return parser.Parse()
}

type dirConfigSourceCreator struct {
	fileSystem    afero.Fs
	parserFactory ConfigParserFactory
	config        struct {
		Type      string
		Path      string
		Format    string
		Recursive bool
	}
}

var _ ConfigSourceCreator = &dirConfigSourceCreator{}

func newDirConfigSourceCreator(fileSystem afero.Fs, parserFactory ConfigParserFactory) ConfigSourceCreator {
	return &dirConfigSourceCreator{
		fileSystem:    fileSystem,
		parserFactory: parserFactory,
	}
}

func (sc *dirConfigSourceCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.Path = ""
	sc.config.Format = "yaml"
	sc.config.Recursive = false
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "dir" &&
		sc.config.Path != "" &&
		sc.config.Format != ""
}

func (sc *dirConfigSourceCreator) Create(_ *Bag) (ConfigSource, error) {
	source := &dirConfigSource{
		configSource: configSource{
			mutex:   &sync.Mutex{},
			partial: Bag{},
		},
		path:          sc.config.Path,
		format:        sc.config.Format,
		recursive:     sc.config.Recursive,
		fileSystem:    sc.fileSystem,
		parserFactory: sc.parserFactory,
	}
	if e := source.load(); e != nil {
		return nil, e
	}
	return source, nil
}

type configRestRequester interface {
	Do(req *http.Request) (*http.Response, error)
}

type restConfigSource struct {
	configSource
	client        configRestRequester
	uri           string
	format        string
	parserFactory ConfigParserFactory
	configPath    string
}

var _ ConfigSource = &restConfigSource{}

func (s *restConfigSource) load() error {
	config, e := s.request()
	if e != nil {
		return e
	}
	p := config.Get(s.configPath, nil)
	if p == nil {
		return NewError("config not found", Bag{"config": config})
	}
	tp, ok := p.(Bag)
	if !ok {
		return NewError("invalid config", Bag{"config": p})
	}
	s.mutex.Lock()
	s.partial = tp
	s.mutex.Unlock()
	return nil
}

func (s *restConfigSource) request() (*Bag, error) {
	var e error
	var req *http.Request
	if req, e = http.NewRequest(http.MethodGet, s.uri, http.NoBody); e != nil {
		return nil, e
	}
	var res *http.Response
	if res, e = s.client.Do(req); e != nil {
		return nil, e
	}
	data, _ := io.ReadAll(res.Body)
	parser, e := s.parserFactory.Create(&Bag{"format": s.format, "reader": bytes.NewReader(data)})
	if e != nil {
		return nil, e
	}
	defer func() {
		if closer, ok := parser.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	return parser.Parse()
}

type restConfigSourceCreator struct {
	clientFactory func() configRestRequester
	parserFactory ConfigParserFactory
	config        struct {
		Type   string
		URI    string
		Format string
		Path   struct {
			Config string
		}
	}
}

var _ ConfigSourceCreator = &restConfigSourceCreator{}

func newRestConfigSourceCreator(parserFactory ConfigParserFactory) ConfigSourceCreator {
	return &restConfigSourceCreator{
		clientFactory: func() configRestRequester { return &http.Client{} },
		parserFactory: parserFactory,
	}
}

func (sc *restConfigSourceCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.URI = ""
	sc.config.Format = "json"
	sc.config.Path.Config = "data.config"
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "rest" &&
		sc.config.URI != "" &&
		sc.config.Format != "" &&
		sc.config.Path.Config != ""
}

func (sc *restConfigSourceCreator) Create(_ *Bag) (ConfigSource, error) {
	source := &restConfigSource{
		configSource: configSource{
			mutex:   &sync.Mutex{},
			partial: Bag{},
		},
		client:        sc.clientFactory(),
		uri:           sc.config.URI,
		format:        sc.config.Format,
		parserFactory: sc.parserFactory,
		configPath:    sc.config.Path.Config,
	}
	if e := source.load(); e != nil {
		return nil, e
	}
	return source, nil
}

type obsRestConfigSource struct {
	restConfigSource
	timestampPath string
	timestamp     time.Time
}

var _ ConfigSource = &obsRestConfigSource{}
var _ ConfigObsSource = &obsRestConfigSource{}

func (s *obsRestConfigSource) Reload() (bool, error) {
	config, e := s.request()
	if e != nil {
		return false, e
	}
	timestamp, e := s.searchTimestamp(config)
	if e != nil {
		return false, e
	}
	if s.timestamp.Equal(time.Unix(0, 0)) || s.timestamp.Before(*timestamp) {
		p := config.Get(s.configPath, nil)
		if p == nil {
			return false, NewError("config not found", Bag{"config": config})
		}
		tp, ok := p.(Bag)
		if !ok {
			return false, NewError("invalid config", Bag{"config": p})
		}
		s.mutex.Lock()
		s.partial = tp
		s.timestamp = *timestamp
		s.mutex.Unlock()
		return true, nil
	}
	return false, nil
}

func (s *obsRestConfigSource) searchTimestamp(config *Bag) (*time.Time, error) {
	t := config.Get(s.timestampPath, nil)
	if t == nil {
		return nil, NewError("timestamp not found", Bag{"config": config})
	}
	tt, ok := t.(string)
	if !ok {
		return nil, NewError("invalid timestamp value", Bag{"timestamp": t})
	}
	timestamp, e := time.Parse(time.RFC3339, tt)
	if e != nil {
		return nil, e
	}
	return &timestamp, nil
}

type obsRestConfigSourceCreator struct {
	clientFactory func() configRestRequester
	parserFactory ConfigParserFactory
	config        struct {
		Type   string
		URI    string
		Format string
		Path   struct {
			Config    string
			Timestamp string
		}
	}
}

var _ ConfigSourceCreator = &obsRestConfigSourceCreator{}

func newObsRestConfigSourceCreator(parserFactory ConfigParserFactory) ConfigSourceCreator {
	return &obsRestConfigSourceCreator{
		clientFactory: func() configRestRequester { return &http.Client{} },
		parserFactory: parserFactory,
	}
}

func (sc *obsRestConfigSourceCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.URI = ""
	sc.config.Format = "json"
	sc.config.Path.Config = "data.config"
	sc.config.Path.Timestamp = "data.timestamp"
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "obs-rest" &&
		sc.config.URI != "" &&
		sc.config.Format != "" &&
		sc.config.Path.Timestamp != "" &&
		sc.config.Path.Config != ""
}

func (sc *obsRestConfigSourceCreator) Create(_ *Bag) (ConfigSource, error) {
	source := &obsRestConfigSource{
		restConfigSource: restConfigSource{
			configSource: configSource{
				mutex:   &sync.Mutex{},
				partial: Bag{},
			},
			client:        sc.clientFactory(),
			uri:           sc.config.URI,
			format:        sc.config.Format,
			parserFactory: sc.parserFactory,
			configPath:    sc.config.Path.Config,
		},
		timestampPath: sc.config.Path.Timestamp,
		timestamp:     time.Unix(0, 0),
	}
	if _, e := source.Reload(); e != nil {
		return nil, e
	}
	return source, nil
}

// ConfigObserver @todo doc
type ConfigObserver func(old, new interface{})

// ConfigManager @todo doc
type ConfigManager interface {
	io.Closer
	Config

	HasSource(id string) bool
	AddSource(id string, priority int, source ConfigSource) error
	RemoveSource(id string) error
	RemoveAllSources() error
	Source(id string) (ConfigSource, error)
	SourcePriority(id string, priority int) error
	HasObserver(path string) bool
	AddObserver(path string, callback ConfigObserver) error
	RemoveObserver(path string)
}

type configSourceRef struct {
	id       string
	priority int
	source   ConfigSource
}

type configSourceRefSorter []configSourceRef

func (source configSourceRefSorter) Len() int {
	return len(source)
}

func (source configSourceRefSorter) Swap(i, j int) {
	source[i], source[j] = source[j], source[i]
}

func (source configSourceRefSorter) Less(i, j int) bool {
	return source[i].priority < source[j].priority
}

type configObserverRef struct {
	path     string
	current  interface{}
	callback ConfigObserver
}

type configManager struct {
	sources   []configSourceRef
	observers []configObserverRef
	partial   Bag
	local     Bag
	mutex     sync.Locker
}

var _ ConfigManager = &configManager{}

func newConfigManager() *configManager {
	return &configManager{
		sources:   []configSourceRef{},
		observers: []configObserverRef{},
		partial:   Bag{},
		local:     Bag{},
		mutex:     &sync.Mutex{},
	}
}

func (m *configManager) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, ref := range m.sources {
		if source, ok := ref.source.(io.Closer); ok {
			if e := source.Close(); e != nil {
				return e
			}
		}
	}
	return nil
}

func (m *configManager) Clone() *Bag {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.Clone()
}

func (m *configManager) Entries() []string {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.Entries()
}

func (m *configManager) Has(path string) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.Has(path)
}

func (m *configManager) Get(path string, def interface{}) interface{} {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.Get(path, def)
}

func (m *configManager) Set(path string, value interface{}) error {
	if e := m.local.Set(path, value); e != nil {
		return e
	}
	m.rebuild()
	return nil
}

func (m *configManager) Bool(path string, def bool) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.Bool(path, def)
}

func (m *configManager) Int(path string, def int) int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.Int(path, def)
}

func (m *configManager) Float(path string, def float64) float64 {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.Float(path, def)
}

func (m *configManager) String(path, def string) string {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.String(path, def)
}

func (m *configManager) List(path string, def []interface{}) []interface{} {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.List(path, def)
}

func (m *configManager) Bag(path string, def *Bag) *Bag {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.Bag(path, def)
}

func (m *configManager) Merge(src Bag) *Bag {
	m.local.Merge(src)
	m.rebuild()
	return &m.partial
}

func (m *configManager) Populate(path string, data interface{}, insensitive ...bool) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.partial.Populate(path, data, insensitive...)
}

func (m *configManager) HasSource(id string) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, ref := range m.sources {
		if ref.id == id {
			return true
		}
	}
	return false
}

func (m *configManager) AddSource(id string, priority int, source ConfigSource) error {
	if m.HasSource(id) {
		return NewError("duplicate source", Bag{"id": id})
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.sources = append(m.sources, configSourceRef{id, priority, source})
	sort.Sort(configSourceRefSorter(m.sources))
	m.rebuild()
	return nil
}

func (m *configManager) RemoveSource(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for i, ref := range m.sources {
		if ref.id != id {
			continue
		}
		if source, ok := ref.source.(io.Closer); ok {
			if e := source.Close(); e != nil {
				return e
			}
		}
		m.sources = append(m.sources[:i], m.sources[i+1:]...)
		m.rebuild()
		return nil
	}
	return nil
}

func (m *configManager) RemoveAllSources() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, ref := range m.sources {
		if source, ok := ref.source.(io.Closer); ok {
			if e := source.Close(); e != nil {
				return e
			}
		}
	}
	m.sources = []configSourceRef{}
	m.rebuild()
	return nil
}

func (m *configManager) Source(id string) (ConfigSource, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, ref := range m.sources {
		if ref.id == id {
			return ref.source, nil
		}
	}
	return nil, NewError("source not found", Bag{"id": id})
}

func (m *configManager) SourcePriority(id string, priority int) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for i, ref := range m.sources {
		if ref.id != id {
			continue
		}
		m.sources[i] = configSourceRef{
			id:       ref.id,
			priority: priority,
			source:   ref.source,
		}
		sort.Sort(configSourceRefSorter(m.sources))
		m.rebuild()
		return nil
	}
	return NewError("source not found", Bag{"id": id})
}

func (m *configManager) HasObserver(path string) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, observer := range m.observers {
		if observer.path == path {
			return true
		}
	}
	return false
}

func (m *configManager) AddObserver(path string, callback ConfigObserver) error {
	val := m.Get(path, nil)
	if val == nil {
		return NewError("invalid path", Bag{"path": path})
	}
	if v, ok := val.(Bag); ok {
		val = v.Clone()
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.observers = append(m.observers, configObserverRef{path, val, callback})
	return nil
}

func (m *configManager) RemoveObserver(path string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for i, observer := range m.observers {
		if observer.path == path {
			m.observers = append(m.observers[:i], m.observers[i+1:]...)
			return
		}
	}
}

func (m *configManager) rebuild() {
	updated := Bag{}
	for _, ref := range m.sources {
		config := ref.source.Get("", Bag{}).(Bag)
		updated.Merge(config)
	}
	updated.Merge(m.local)
	m.partial = updated
	for id, observer := range m.observers {
		val := m.partial.Get(observer.path, nil)
		if val != nil && !reflect.DeepEqual(observer.current, val) {
			old := observer.current
			m.observers[id].current = val
			observer.callback(old, val)
		}
	}
}

func (m *configManager) reload() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	reloaded := false
	for _, ref := range m.sources {
		if source, ok := ref.source.(ConfigObsSource); ok {
			updated, e := source.Reload()
			if e != nil {
				return e
			}
			reloaded = reloaded || updated
		}
	}
	if reloaded {
		m.rebuild()
	}
	return nil
}

type configLoader struct {
	sourceFactory ConfigSourceFactory
	manager       *configManager
}

func newConfigLoader(sourceFactory ConfigSourceFactory, manager *configManager) *configLoader {
	return &configLoader{
		sourceFactory: sourceFactory,
		manager:       manager,
	}
}

func (l *configLoader) load() error {
	if !ConfigLoaderActive {
		return nil
	}
	source, e := l.sourceFactory.Create(&Bag{
		"type":   "file",
		"path":   ConfigLoaderFilePath,
		"format": ConfigLoaderFileFormat,
	})
	if e != nil {
		return e
	}
	if e = l.manager.AddSource(ConfigLoaderSourceID, 0, source); e != nil {
		return e
	}
	config := struct {
		Sources Bag
	}{}
	if e := l.manager.Populate(ConfigLoaderConfigPath, &config); e != nil {
		return nil
	}
	for id, c := range config.Sources {
		tc, ok := c.(Bag)
		if !ok {
			continue
		}
		if e := l.loadSource(id, tc); e != nil {
			return e
		}
	}
	return nil
}

func (l *configLoader) loadSource(id string, config Bag) error {
	source, e := l.sourceFactory.Create(&config)
	if e != nil {
		return e
	}
	priority := 0
	_ = config.Populate("priority", &priority)
	return l.manager.AddSource(id, priority, source)
}

type configInitializer struct {
	manager  *configManager
	loader   *configLoader
	observer Trigger
}

func newConfigInitializer(manager *configManager, loader *configLoader) *configInitializer {
	return &configInitializer{
		manager:  manager,
		loader:   loader,
		observer: nil,
	}
}

func (i *configInitializer) init() error {
	if e := i.loader.load(); e != nil {
		return e
	}
	if ConfigObserveFrequency != 0 {
		period := time.Duration(ConfigObserveFrequency) * time.Millisecond
		i.observer = newRecurringTrigger(period, func() error {
			return i.manager.reload()
		})
	}
	return nil
}

func (i *configInitializer) close() error {
	if i.observer != nil {
		return i.observer.Close()
	}
	return nil
}

type configProvider struct {
	app App
}

var _ Provider = &configProvider{}

func newConfigProvider() *configProvider {
	return &configProvider{}
}

func (*configProvider) ID() string {
	return "flam.config"
}

func (p *configProvider) Reg(app App) error {
	_ = app.DI().Provide(newConfigParserFactory)
	_ = app.DI().Provide(func() ConfigParserCreator { return &jsonConfigParserCreator{} }, dig.Group(ConfigParserTag))
	_ = app.DI().Provide(func() ConfigParserCreator { return &yamlConfigParserCreator{} }, dig.Group(ConfigParserTag))
	_ = app.DI().Provide(newConfigSourceFactory)
	_ = app.DI().Provide(newAggregateConfigSourceCreator, dig.Group(ConfigSourceTag))
	_ = app.DI().Provide(newEnvConfigSourceCreator, dig.Group(ConfigSourceTag))
	_ = app.DI().Provide(newFileConfigSourceCreator, dig.Group(ConfigSourceTag))
	_ = app.DI().Provide(newObsFileConfigSourceCreator, dig.Group(ConfigSourceTag))
	_ = app.DI().Provide(newDirConfigSourceCreator, dig.Group(ConfigSourceTag))
	_ = app.DI().Provide(newRestConfigSourceCreator, dig.Group(ConfigSourceTag))
	_ = app.DI().Provide(newObsRestConfigSourceCreator, dig.Group(ConfigSourceTag))
	_ = app.DI().Provide(newConfigManager)
	_ = app.DI().Provide(func(m *configManager) ConfigManager { return m })
	_ = app.DI().Provide(func(m *configManager) Config { return m })
	_ = app.DI().Provide(newConfigLoader)
	_ = app.DI().Provide(newConfigInitializer)
	p.app = app
	return nil
}

func (p *configProvider) Boot() error {
	return p.app.DI().Invoke(func(i *configInitializer) error {
		return i.init()
	})
}

func (p *configProvider) Close() error {
	return p.app.DI().Invoke(func(i *configInitializer) error {
		return i.close()
	})
}
