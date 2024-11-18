set dotenv-load

version := `git describe --tags`

build:
    go build -ldflags "-X ransan.fr/zimbridge/config.Version={{version}}" ransan.fr/zimbridge/cmd/mda

run *ARGS: build
    ./mda {{ARGS}}
