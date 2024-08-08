package flam

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/afero"
	"go.uber.org/dig"
)

const (
	// LogFormatterTag @todo doc
	LogFormatterTag = "flam.log.formatter"

	// LogStreamTag @todo doc
	LogStreamTag = "flam.log.stream"
)

var (
	// LogLoaderActive @todo doc
	LogLoaderActive = EnvBool("FLAMINGO_LOG_LOADER_ACTIVE", true)

	// LogLoaderStreamListConfigPath @todo doc
	LogLoaderStreamListConfigPath = EnvString("FLAMINGO_LOG_LOADER_STREAM_LIST_CONFIG_PATH", "flam.log.streams")

	// LogFlushFrequency @todo doc
	LogFlushFrequency = EnvInt("FLAMINGO_LOG_FLUSH_FREQUENCY", 0)
)

// LogLevel @todo doc
type LogLevel int

const (
	// LogFatal @todo doc
	LogFatal LogLevel = 1 + iota
	// LogError @todo doc
	LogError
	// LogWarning @todo doc
	LogWarning
	// LogNotice @todo doc
	LogNotice
	// LogInfo @todo doc
	LogInfo
	// LogDebug @todo doc
	LogDebug
)

// LogLevelMap @todo doc
var LogLevelMap = map[string]LogLevel{
	"fatal":   LogFatal,
	"error":   LogError,
	"warning": LogWarning,
	"notice":  LogNotice,
	"info":    LogInfo,
	"debug":   LogDebug,
}

// LogLevelName @todo doc
var LogLevelName = map[LogLevel]string{
	LogFatal:   "fatal",
	LogError:   "error",
	LogWarning: "warning",
	LogNotice:  "notice",
	LogInfo:    "info",
	LogDebug:   "debug",
}

// Logger @todo doc
type Logger interface {
	Signal(level LogLevel, channel, msg string, ctx ...Bag) error
	Broadcast(level LogLevel, msg string, ctx ...Bag) error
	Flush() error
}

// LogFormatter @todo doc
type LogFormatter interface {
	Format(level LogLevel, message string, ctx ...Bag) string
}

// LogFormatterCreator @todo doc
type LogFormatterCreator interface {
	Accept(config *Bag) bool
	Create(config *Bag) (LogFormatter, error)
}

// LogFormatterFactory @todo doc
type LogFormatterFactory interface {
	AddCreator(creator LogFormatterCreator) LogFormatterFactory
	Create(config *Bag) (LogFormatter, error)
}

type logFormatterFactory []LogFormatterCreator

var _ LogFormatterFactory = &logFormatterFactory{}

func newLogFormatterFactory(
	args struct {
		dig.In
		Creators []LogFormatterCreator `group:"flam.log.formatter"`
	},
) LogFormatterFactory {
	factory := &logFormatterFactory{}
	for _, creator := range args.Creators {
		*factory = append(*factory, creator)
	}
	return factory
}

func (f *logFormatterFactory) AddCreator(creator LogFormatterCreator) LogFormatterFactory {
	*f = append(*f, creator)
	return f
}

func (f *logFormatterFactory) Create(config *Bag) (LogFormatter, error) {
	for _, creator := range *f {
		if creator.Accept(config) {
			return creator.Create(config)
		}
	}
	return nil, NewError("unable to parse formatter config", Bag{"config": config})
}

type jsonLogFormatter struct{}

var _ LogFormatter = &jsonLogFormatter{}

func (f jsonLogFormatter) Format(level LogLevel, message string, ctx ...Bag) string {
	context := Bag{}
	for _, c := range ctx {
		context.Merge(c)
	}
	context["time"] = time.Now().Format("2006-01-02T15:04:05.000-0700")
	context["level"] = strings.ToUpper(LogLevelName[level])
	context["message"] = message
	bytes, _ := json.Marshal(context)
	return string(bytes)
}

type jsonLogFormatterCreator struct {
	config struct {
		Format string
	}
}

var _ LogFormatterCreator = &jsonLogFormatterCreator{}

func (fc *jsonLogFormatterCreator) Accept(config *Bag) bool {
	fc.config.Format = "json"
	if e := config.Populate("", &fc.config); e != nil {
		return false
	}
	return strings.ToLower(fc.config.Format) == "json"
}

func (fc *jsonLogFormatterCreator) Create(_ *Bag) (LogFormatter, error) {
	return &jsonLogFormatter{}, nil
}

// LogStream @todo doc
type LogStream interface {
	Logger

	HasChannel(channel string) bool
	ListChannels() []string
	AddChannel(channel string)
	RemoveChannel(channel string)
}

// LogStreamCreator @todo doc
type LogStreamCreator interface {
	Accept(config *Bag) bool
	Create(config *Bag) (LogStream, error)
}

// LogStreamFactory @todo doc
type LogStreamFactory interface {
	AddCreator(creator LogStreamCreator) LogStreamFactory
	Create(config *Bag) (LogStream, error)
}

type logStreamFactory []LogStreamCreator

var _ LogStreamFactory = &logStreamFactory{}

func newLogStreamFactory(
	args struct {
		dig.In
		Creators []LogStreamCreator `group:"flam.log.stream"`
	},
) LogStreamFactory {
	factory := &logStreamFactory{}
	for _, creator := range args.Creators {
		*factory = append(*factory, creator)
	}
	return factory
}

func (f *logStreamFactory) AddCreator(creator LogStreamCreator) LogStreamFactory {
	*f = append(*f, creator)
	return f
}

func (f *logStreamFactory) Create(config *Bag) (LogStream, error) {
	for _, creator := range *f {
		if creator.Accept(config) {
			return creator.Create(config)
		}
	}
	return nil, NewError("unable to parse stream config", Bag{"config": config})
}

type logStreamCreator struct {
	formatterFactory LogFormatterFactory
}

func (logStreamCreator) level(level string) (LogLevel, error) {
	level = strings.ToLower(level)
	if _, ok := LogLevelMap[level]; !ok {
		return LogFatal, NewError("invalid log level", Bag{"level": level})
	}
	return LogLevelMap[level], nil
}

func (logStreamCreator) channels(list []interface{}) []string {
	var result []string
	for _, channel := range list {
		if typedChannel, ok := channel.(string); ok {
			result = append(result, typedChannel)
		}
	}
	return result
}

type logStream struct {
	Channels  []string
	Level     LogLevel
	Formatter LogFormatter
	Mutex     sync.Locker
	Buffer    []string
	Writer    io.Writer
}

var _ LogStream = &logStream{}

func (s *logStream) Close() error {
	return s.Flush()
}

func (s *logStream) Signal(level LogLevel, channel, msg string, ctx ...Bag) error {
	i := sort.SearchStrings(s.Channels, channel)
	if i == len(s.Channels) || s.Channels[i] != channel {
		return nil
	}
	ctx = append(ctx, Bag{"channel": channel})
	return s.Broadcast(level, msg, ctx...)
}

func (s *logStream) Broadcast(level LogLevel, msg string, ctx ...Bag) error {
	if s.Level < level {
		return nil
	}
	s.Buffer = append(s.Buffer, s.Format(level, msg, ctx...))
	return nil
}

func (s *logStream) Flush() error {
	for _, line := range s.Buffer {
		_, e := fmt.Fprintln(s.Writer, line)
		if e != nil {
			return e
		}
	}
	s.Buffer = nil
	return nil
}

func (s *logStream) HasChannel(channel string) bool {
	i := sort.SearchStrings(s.Channels, channel)
	return i < len(s.Channels) && s.Channels[i] == channel
}

func (s *logStream) ListChannels() []string {
	return s.Channels
}

func (s *logStream) AddChannel(channel string) {
	if !s.HasChannel(channel) {
		s.Channels = append(s.Channels, channel)
		sort.Strings(s.Channels)
	}
}

func (s *logStream) RemoveChannel(channel string) {
	i := sort.SearchStrings(s.Channels, channel)
	if i == len(s.Channels) || s.Channels[i] != channel {
		return
	}
	s.Channels = append(s.Channels[:i], s.Channels[i+1:]...)
}

func (s *logStream) Format(level LogLevel, message string, ctx ...Bag) string {
	if s.Formatter != nil {
		message = s.Formatter.Format(level, message, ctx...)
	}
	return message
}

type consoleLogStream struct {
	logStream
}

var _ LogStream = &consoleLogStream{}

type consoleLogStreamCreator struct {
	logStreamCreator
	config struct {
		Type     string
		Level    string
		Format   string
		Channels []interface{}
	}
}

var _ LogStreamCreator = &consoleLogStreamCreator{}

func newConsoleLogStreamCreator(formatterFactory LogFormatterFactory) LogStreamCreator {
	return &consoleLogStreamCreator{
		logStreamCreator: logStreamCreator{
			formatterFactory: formatterFactory,
		},
	}
}

func (sc *consoleLogStreamCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.Level = LogLevelName[LogWarning]
	sc.config.Format = "json"
	sc.config.Channels = []interface{}{}
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "console" &&
		sc.config.Level != "" &&
		sc.config.Format != ""
}

func (sc *consoleLogStreamCreator) Create(_ *Bag) (LogStream, error) {
	level, e := sc.level(sc.config.Level)
	if e != nil {
		return nil, e
	}
	formatter, e := sc.formatterFactory.Create(&Bag{"format": sc.config.Format})
	if e != nil {
		return nil, e
	}
	stream := &consoleLogStream{
		logStream: logStream{
			Channels:  sc.channels(sc.config.Channels),
			Level:     level,
			Formatter: formatter,
			Mutex:     &sync.Mutex{},
			Writer:    os.Stdout,
		},
	}
	sort.Strings(stream.Channels)
	return stream, nil
}

type fileLogStream struct {
	logStream
}

var _ LogStream = &fileLogStream{}

func (s *fileLogStream) Close() error {
	_ = s.Flush()
	if s.Writer != nil {
		if closer, ok := s.Writer.(io.Closer); ok {
			_ = closer.Close()
		}
		s.Writer = nil
	}
	return nil
}

type fileLogStreamCreator struct {
	logStreamCreator
	fileSystem afero.Fs
	config     struct {
		Type     string
		Path     string
		Level    string
		Format   string
		Channels []interface{}
	}
}

var _ LogStreamCreator = &fileLogStreamCreator{}

func newFileLogStreamCreator(fileSystem afero.Fs, formatterFactory LogFormatterFactory) LogStreamCreator {
	return &fileLogStreamCreator{
		logStreamCreator: logStreamCreator{
			formatterFactory: formatterFactory,
		},
		fileSystem: fileSystem,
	}
}

func (sc *fileLogStreamCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.Path = ""
	sc.config.Level = LogLevelName[LogWarning]
	sc.config.Format = "json"
	sc.config.Channels = []interface{}{}
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "file" &&
		sc.config.Path != "" &&
		sc.config.Level != "" &&
		sc.config.Format != ""
}

func (sc *fileLogStreamCreator) Create(_ *Bag) (LogStream, error) {
	level, e := sc.level(sc.config.Level)
	if e != nil {
		return nil, e
	}
	formatter, e := sc.formatterFactory.Create(&Bag{"format": sc.config.Format})
	if e != nil {
		return nil, e
	}
	file, e := sc.fileSystem.OpenFile(sc.config.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if e != nil {
		return nil, e
	}
	stream := &fileLogStream{
		logStream: logStream{
			Channels:  sc.channels(sc.config.Channels),
			Level:     level,
			Formatter: formatter,
			Mutex:     &sync.Mutex{},
			Writer:    file,
		},
	}
	sort.Strings(stream.Channels)
	return stream, nil
}

type logRotatingFileWriter struct {
	lock       sync.Locker
	fileSystem afero.Fs
	fp         afero.File
	file       string
	current    string
	year       int
	month      time.Month
	day        int
}

var _ io.Writer = &logRotatingFileWriter{}

func newLogRotatingFileWriter(fs afero.Fs, file string) (io.Writer, error) {
	writer := &logRotatingFileWriter{
		lock:       &sync.Mutex{},
		fileSystem: fs,
		file:       file,
	}
	if e := writer.rotate(); e != nil {
		return nil, e
	}
	return writer, nil
}

func (w *logRotatingFileWriter) Write(output []byte) (int, error) {
	w.lock.Lock()
	defer w.lock.Unlock()
	if e := w.checkRotation(); e != nil {
		return 0, e
	}
	return w.fp.Write(output)
}

func (w *logRotatingFileWriter) Close() error {
	return w.fp.(io.Closer).Close()
}

func (w *logRotatingFileWriter) checkRotation() error {
	now := time.Now()
	if now.Day() != w.day || now.Month() != w.month || now.Year() != w.year {
		return w.rotate()
	}
	return nil
}

func (w *logRotatingFileWriter) rotate() error {
	var e error
	if w.fp != nil {
		if e = w.fp.(io.Closer).Close(); e != nil {
			w.fp = nil
			return e
		}
	}
	now := time.Now()
	w.year = now.Year()
	w.month = now.Month()
	w.day = now.Day()
	w.current = fmt.Sprintf(w.file, now.Format("2006-01-02"))
	if w.fp, e = w.fileSystem.OpenFile(w.current, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); e != nil {
		return e
	}
	return nil
}

type rotatingFileLogStreamCreator struct {
	fileLogStreamCreator
}

var _ LogStreamCreator = &rotatingFileLogStreamCreator{}

func newRotatingFileLogStreamCreator(fileSystem afero.Fs, formatterFactory LogFormatterFactory) LogStreamCreator {
	return &rotatingFileLogStreamCreator{
		fileLogStreamCreator: fileLogStreamCreator{
			logStreamCreator: logStreamCreator{
				formatterFactory: formatterFactory,
			},
			fileSystem: fileSystem,
		},
	}
}

func (sc *rotatingFileLogStreamCreator) Accept(config *Bag) bool {
	sc.config.Type = ""
	sc.config.Path = ""
	sc.config.Level = LogLevelName[LogWarning]
	sc.config.Format = "json"
	sc.config.Channels = []interface{}{}
	if e := config.Populate("", &sc.config); e != nil {
		return false
	}
	return strings.ToLower(sc.config.Type) == "rotating-file" &&
		sc.config.Path != "" &&
		sc.config.Level != "" &&
		sc.config.Format != ""
}

func (sc *rotatingFileLogStreamCreator) Create(_ *Bag) (LogStream, error) {
	level, e := sc.level(sc.config.Level)
	if e != nil {
		return nil, e
	}
	formatter, e := sc.formatterFactory.Create(&Bag{"format": sc.config.Format})
	if e != nil {
		return nil, e
	}
	file, e := newLogRotatingFileWriter(sc.fileSystem, sc.config.Path)
	if e != nil {
		return nil, e
	}
	stream := &fileLogStream{
		logStream: logStream{
			Channels:  sc.channels(sc.config.Channels),
			Level:     level,
			Formatter: formatter,
			Mutex:     &sync.Mutex{},
			Writer:    file,
		},
	}
	sort.Strings(stream.Channels)
	return stream, nil
}

// LogManager @todo doc
type LogManager interface {
	Logger

	HasStream(id string) bool
	ListStreams() []string
	AddStream(id string, stream LogStream) error
	RemoveStream(id string)
	RemoveAllStreams()
	Stream(id string) (LogStream, error)
}

type logManager struct {
	streams map[string]LogStream
	mutex   sync.Locker
}

var _ Logger = &logManager{}
var _ LogManager = &logManager{}

func newLogManager() *logManager {
	return &logManager{
		mutex:   &sync.Mutex{},
		streams: map[string]LogStream{},
	}
}

func (m *logManager) Close() error {
	if e := m.Flush(); e != nil {
		return e
	}
	m.RemoveAllStreams()
	return nil
}

func (m *logManager) Signal(level LogLevel, channel, msg string, ctx ...Bag) error {
	m.mutex.Lock()
	defer func() { m.mutex.Unlock() }()
	for _, s := range m.streams {
		if e := s.Signal(level, channel, msg, ctx...); e != nil {
			return e
		}
	}
	return nil
}

func (m *logManager) Broadcast(level LogLevel, msg string, ctx ...Bag) error {
	m.mutex.Lock()
	defer func() { m.mutex.Unlock() }()
	for _, s := range m.streams {
		if e := s.Broadcast(level, msg, ctx...); e != nil {
			return e
		}
	}
	return nil
}

func (m *logManager) Flush() error {
	m.mutex.Lock()
	defer func() { m.mutex.Unlock() }()
	for _, s := range m.streams {
		if e := s.Flush(); e != nil {
			return e
		}
	}
	return nil
}

func (m *logManager) HasStream(id string) bool {
	m.mutex.Lock()
	defer func() { m.mutex.Unlock() }()
	_, ok := m.streams[id]
	return ok
}

func (m *logManager) ListStreams() []string {
	m.mutex.Lock()
	defer func() { m.mutex.Unlock() }()
	var list []string
	for id := range m.streams {
		list = append(list, id)
	}
	return list
}

func (m *logManager) AddStream(id string, stream LogStream) error {
	if m.HasStream(id) {
		return NewError("duplicate stream", Bag{"id": id})
	}
	m.mutex.Lock()
	defer func() { m.mutex.Unlock() }()
	m.streams[id] = stream
	return nil
}

func (m *logManager) RemoveStream(id string) {
	m.mutex.Lock()
	defer func() { m.mutex.Unlock() }()
	if s, ok := m.streams[id]; ok {
		if closer, ok := s.(io.Closer); ok {
			_ = closer.Close()
		}
		delete(m.streams, id)
	}
}

func (m *logManager) RemoveAllStreams() {
	m.mutex.Lock()
	defer func() { m.mutex.Unlock() }()
	for id, s := range m.streams {
		if closer, ok := s.(io.Closer); ok {
			_ = closer.Close()
		}
		delete(m.streams, id)
	}
}

func (m *logManager) Stream(id string) (LogStream, error) {
	m.mutex.Lock()
	defer func() { m.mutex.Unlock() }()
	if writer, ok := m.streams[id]; ok {
		return writer, nil
	}
	return nil, NewError("stream not found", Bag{"id": id})
}

type logLoader struct {
	config        Config
	streamFactory LogStreamFactory
	manager       *logManager
}

func newLogLoader(config Config, streamFactory LogStreamFactory, manager *logManager) *logLoader {
	return &logLoader{
		config:        config,
		manager:       manager,
		streamFactory: streamFactory,
	}
}

func (l logLoader) load() error {
	if !LogLoaderActive {
		return nil
	}
	config := Bag{}
	if e := l.config.Populate(LogLoaderStreamListConfigPath, &config); e != nil {
		return nil
	}
	for id, c := range config {
		tc, ok := c.(Bag)
		if !ok {
			continue
		}
		if e := l.loadStream(id, &tc); e != nil {
			return e
		}
	}
	return nil
}

func (l logLoader) loadStream(id string, config *Bag) error {
	stream, e := l.streamFactory.Create(config)
	if e != nil {
		return e
	}
	return l.manager.AddStream(id, stream)
}

type logInitializer struct {
	manager *logManager
	loader  *logLoader
	flusher Trigger
}

func newLogInitializer(manager *logManager, loader *logLoader) *logInitializer {
	return &logInitializer{
		manager: manager,
		loader:  loader,
		flusher: nil,
	}
}

func (i *logInitializer) init() error {
	if e := i.loader.load(); e != nil {
		return e
	}
	if LogFlushFrequency != 0 {
		period := time.Duration(LogFlushFrequency) * time.Millisecond
		i.flusher = newRecurringTrigger(period, func() error {
			return i.manager.Flush()
		})
	}
	return nil
}

func (i *logInitializer) close() error {
	_ = i.manager.Flush()
	if i.flusher != nil {
		return i.flusher.Close()
	}
	return nil
}

type logProvider struct {
	app App
}

var _ Provider = &logProvider{}

func newLogProvider() *logProvider {
	return &logProvider{}
}

func (*logProvider) ID() string {
	return "flam.log"
}

func (p *logProvider) Reg(app App) error {
	_ = app.DI().Provide(newLogFormatterFactory)
	_ = app.DI().Provide(func() LogFormatterCreator { return &jsonLogFormatterCreator{} }, dig.Group(LogFormatterTag))
	_ = app.DI().Provide(newLogStreamFactory)
	_ = app.DI().Provide(newConsoleLogStreamCreator, dig.Group(LogStreamTag))
	_ = app.DI().Provide(newFileLogStreamCreator, dig.Group(LogStreamTag))
	_ = app.DI().Provide(newRotatingFileLogStreamCreator, dig.Group(LogStreamTag))
	_ = app.DI().Provide(newLogManager)
	_ = app.DI().Provide(func(m *logManager) LogManager { return m })
	_ = app.DI().Provide(func(m *logManager) Logger { return m })
	_ = app.DI().Provide(newLogLoader)
	_ = app.DI().Provide(newLogInitializer)
	p.app = app
	return nil
}

func (p *logProvider) Boot() error {
	return p.app.DI().Invoke(func(i *logInitializer) error {
		return i.init()
	})
}

func (p *logProvider) Close() error {
	return p.app.DI().Invoke(func(i *logInitializer) error {
		return i.close()
	})
}
