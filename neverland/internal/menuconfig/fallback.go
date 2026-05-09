package menuconfig

// FallbackTheme is served when git is unreachable. Uses Kontango brand colors.
func FallbackTheme() ThemeConfig {
	return ThemeConfig{
		Title:   "Kontango Boot",
		Tagline: "Boot anywhere. Own everything.",
		LogoASCII: []string{
			"  |/ _ |\\ | |_ /\\  |\\ |  /__ _ ",
			"  |\\(_)| \\|  | /--\\ | \\| /  (_)",
		},
		LogoPNGURL: "",
		Colors: ThemeColors{
			Background:  "0x0f4c5c",
			Foreground:  "0xffffff",
			HighlightBg: "0xe6b94d",
			HighlightFg: "0x1f2024",
			GapText:     "0x6b6f7a",
		},
		TimeoutSeconds: 30,
		DefaultEntry:   "hookos",
	}
}

// FallbackEntries is served when git is unreachable.
// Contains exactly: Hook OS install, rescue shell.
// Built-in local/shell entries are appended by the renderer regardless.
func FallbackEntries() EntriesConfig {
	return EntriesConfig{
		Entries: []EntryConfig{
			{
				ID:      "hookos",
				Label:   "Install Kontango Hook OS",
				Key:     "1",
				Type:    "hook",
				Variant: "",
				Arch:    []string{"x86_64", "i386", "aarch64"},
				Enabled: true,
			},
			{
				ID:      "rescue",
				Label:   "Rescue shell",
				Key:     "r",
				Type:    "hook",
				Variant: "rescue",
				Arch:    []string{"x86_64", "i386", "aarch64"},
				Enabled: true,
			},
		},
	}
}
