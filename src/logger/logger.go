package logger

import (
	"log"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/mitchellh/go-homedir"
)

// Global logger - accessible from anywhere
var Debug *log.Logger
var StatusChan chan string
var HistoryPersistedChan chan int64

// Init sets up the logger - call this from main
func Init(filename string) error {
	expandedPath, err := homedir.Expand(filename)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(expandedPath), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(expandedPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	Debug = log.New(f, "", log.LstdFlags|log.Lshortfile)
	Debug.Println("Logger initialized")
	return nil
}

// Screen outputs text to screen (CLI) or sends to TUI via channel
func Screen(text string, color *color.Color) {
	// If TUI channel exists, send status message there
	if StatusChan != nil {
		// Non-blocking send - don't hang if channel is full
		select {
		case StatusChan <- text:
		default:
			// Channel full, drop message
		}
	} else {
		// Normal CLI output
		if color != nil {
			color.Print(text)
		} else {
			print(text)
		}
	}
}

// HistoryPersisted notifies TUI listeners when a history row is saved.
func HistoryPersisted(contextId int64) {
	if HistoryPersistedChan == nil {
		return
	}

	select {
	case HistoryPersistedChan <- contextId:
	default:
		// Channel full, drop message
	}
}
