allow_k8s_contexts('kind-kind')

local_resource(
    'go-build',
    cmd='mkdir -p ./bin && GOOS=linux GOARCH=amd64 go build -o ./bin/manager ./cmd/main.go',
    deps=['cmd', 'internal', 'go.mod', 'go.sum'],
)

docker_build(
    'cdmdckr/redis-operator',
    '.',
    dockerfile='Dockerfile.dev',
    only=[
        'Dockerfile.dev',
        'terraform/',
        'bin/manager',
    ],
    live_update=[
        sync('./bin/manager', '/manager'),
        run('killall manager || true'),
    ],
)

k8s_yaml(kustomize('config/default'))
k8s_resource('fgt-operator', port_forwards=8085)