//go:build !windows

package main

func attachConsole() {}

func fatalUI(msg string) {}

func canChooseMarkdownFile() bool { return false }

func chooseMarkdownFile() (string, bool) { return "", false }
