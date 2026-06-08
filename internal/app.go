package internal

// App holds all dependencies for command execution.
type App struct {
	Config *Config
	DB     *DB
	Git    *GitClient
}


