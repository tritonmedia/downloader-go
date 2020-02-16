local circle = import 'circle.libsonnet';

circle.ServiceConfig('downloader-go') {
  jobs+: {
    tests: circle.Job(dockerImage='circleci/golang:1.13', withDocker=false) {
      steps_+:: [
        circle.RestoreCacheStep('go-deps-{{ checksum "go.sum" }}'),
        circle.RunStep('Fetch Dependencies', 'go mod vendor'),
        circle.RunStep('Run Tests', 'make test'),
        // put save_cache step here thanks to make test downloading deps...
        circle.SaveCacheStep('go-deps-{{ checksum "go.sum" }}', ['/go/pkg/mod']),
      ],
    },
  },
  workflows+: {
    ['build-push']+: {
      jobs_:: [
        'tests', 
        {
          name:: 'build',
          branches: {
            only: ['master'],
          },
          requires: ['tests'],
        }
      ],
    },
  },
}