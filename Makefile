.PHONY: build sign restart

build:
	cd src && go build -o ../bot .
	cp bot Artoo.app/Contents/MacOS/bot
	codesign --force --deep --sign "Max Flach" Artoo.app

sign:
	codesign --force --deep --sign "Max Flach" Artoo.app

restart:
	launchctl unload ~/Library/LaunchAgents/com.bot.claude.default.plist
	launchctl load ~/Library/LaunchAgents/com.bot.claude.default.plist
