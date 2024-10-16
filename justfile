set dotenv-load

version := `git describe --tags`

build:
    go build -ldflags "-X ransan.fr/zimbridge/mda/config.Version={{version}}"

run *ARGS: build
    ./mda {{ARGS}}
