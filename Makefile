BINARY     := chord
MAIN       := ./cmd/chord
INSTALL_DIR ?= $(HOME)/.local/bin

.PHONY: build install clean

build:
	go build -o $(BINARY) $(MAIN)

install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed $(BINARY) → $(INSTALL_DIR)/$(BINARY)"

clean:
	rm -f $(BINARY)
