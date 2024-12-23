dev:
    go run .
    
build:
    go build -o ./chronicle

deploy_lmdb target:
    docker build -f "compileDockerfile" --platform linux/amd64 -t chronicle-app .
    docker create --name temp-container --platform linux/amd64 chronicle-app
    docker cp temp-container:/app/chronicle-linux-amd64 ./chronicle-linux-amd64
    docker rm temp-container
    rsync --progress chronicle-linux-amd64 {{target}}:chronicle/chronicle-new
    ssh {{target}} 'systemctl stop chronicle'
    ssh {{target}} 'mv chronicle/chronicle-new chronicle/chronicle'
    ssh {{target}} 'systemctl start chronicle'

deploy target:
    GOOS=linux GOARCH=amd64 go build -o chronicle-linux-amd64
    rsync --progress chronicle-linux-amd64 {{target}}:chronicle/chronicle-new
    ssh {{target}} 'systemctl stop chronicle'
    ssh {{target}} 'mv chronicle/chronicle-new chronicle/chronicle'
    ssh {{target}} 'systemctl start chronicle'
