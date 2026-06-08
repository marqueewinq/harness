package internal

// App holds all dependencies for command execution.
type App struct {
	Config *Config
	DB     *DB
	Git    *GitClient
}

// Stubs -- replaced by real implementations in later tasks
type Config struct{}
type DB struct{}
type GitClient struct{}
