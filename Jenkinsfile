// Jenkins equivalent of .github/workflows/ci.yml + cd.yml, for teams running Jenkins on-prem.
// Requires agents with Docker, plus credentials `registry-creds` (username/password) and
// `staging-kubeconfig` (secret file).
pipeline {
    agent none

    options {
        timestamps()
        timeout(time: 60, unit: 'MINUTES')
        disableConcurrentBuilds(abortPrevious: true)
    }

    environment {
        REGISTRY = 'ghcr.io/satfleet'
        IMAGE_TAG = "${env.GIT_COMMIT?.take(12) ?: 'dev'}"
    }

    stages {
        stage('Unit and integration tests') {
            parallel {
                stage('C agent') {
                    agent { docker { image 'gcc:13' } }
                    steps {
                        dir('agent') {
                            sh 'make test'
                            sh 'make asan'
                        }
                    }
                }
                stage('Go ingest') {
                    agent { docker { image 'golang:1.24' } }
                    steps {
                        dir('services/ingest') {
                            sh 'go vet ./...'
                            sh 'go test -race -count=1 ./...'
                        }
                    }
                }
                stage('Go ingest + Postgres') {
                    agent { label 'docker' }
                    steps {
                        sh '''
                          docker run -d --name pg-$BUILD_TAG -e POSTGRES_USER=fleet -e POSTGRES_PASSWORD=fleet \
                            -e POSTGRES_DB=fleet_test -p 55432:5432 postgres:16-alpine
                          until docker exec pg-$BUILD_TAG pg_isready -U fleet; do sleep 1; done
                          docker run --rm --network host -v "$PWD":/src -w /src/services/ingest \
                            -e TEST_DATABASE_URL='postgres://fleet:fleet@localhost:55432/fleet_test?sslmode=disable' \
                            golang:1.24 go test -tags=integration -count=1 ./internal/store/...
                        '''
                    }
                    post { always { sh 'docker rm -f pg-$BUILD_TAG || true' } }
                }
                stage('Java fleet API') {
                    agent { label 'docker' }
                    steps {
                        // Testcontainers needs the Docker socket for its Postgres container.
                        sh '''
                          docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v "$PWD":/src \
                            -w /src/services/fleet-api maven:3.9-eclipse-temurin-21 mvn -B -ntp verify
                        '''
                    }
                    post { always { junit allowEmptyResults: true, testResults: 'services/fleet-api/target/*-reports/*.xml' } }
                }
                stage('React frontend') {
                    agent { docker { image 'node:22' } }
                    steps {
                        dir('web') {
                            sh 'npm ci --no-audit --no-fund'
                            sh 'npm run lint && npm run typecheck && npm test && npm run build'
                        }
                    }
                }
            }
        }

        stage('End-to-end and load test') {
            agent { label 'docker' }
            environment { COMPOSE_PROJECT_NAME = "satfleet-${env.BUILD_NUMBER}" }
            steps {
                sh 'docker compose up --build --detach --wait --wait-timeout 300'
                sh '''
                  docker run --rm --network host -v "$PWD/e2e":/e2e -w /e2e -e E2E_BASE_URL=http://localhost:3000 \
                    mcr.microsoft.com/playwright:v1.56.1-noble sh -c "npm ci && npx playwright test"
                '''
                sh '''
                  docker compose run --rm --no-deps --entrypoint /loadtest simulator \
                    -target ingest:9090 -metrics http://ingest:8081/metrics \
                    -rate 6000 -duration 60s -min-rate 5000 -json - > loadtest.json
                '''
                archiveArtifacts artifacts: 'loadtest.json, e2e/playwright-report/**', allowEmptyArchive: true
            }
            post {
                failure { sh 'docker compose logs --no-color --tail 300' }
                always { sh 'docker compose down -v || true' }
            }
        }

        stage('Publish images') {
            when { branch 'main' }
            agent { label 'docker' }
            steps {
                withCredentials([usernamePassword(credentialsId: 'registry-creds', usernameVariable: 'REG_USER', passwordVariable: 'REG_PASS')]) {
                    sh '''
                      echo "$REG_PASS" | docker login ghcr.io -u "$REG_USER" --password-stdin
                      docker build -t $REGISTRY/satfleet-fleet-api:$IMAGE_TAG services/fleet-api
                      docker build -t $REGISTRY/satfleet-ingest:$IMAGE_TAG --target ingest services/ingest
                      docker build -t $REGISTRY/satfleet-simulator:$IMAGE_TAG --target simulator services/ingest
                      docker build -t $REGISTRY/satfleet-web:$IMAGE_TAG web
                      docker build -t $REGISTRY/satfleet-agent:$IMAGE_TAG agent
                      for n in fleet-api ingest simulator web agent; do docker push $REGISTRY/satfleet-$n:$IMAGE_TAG; done
                    '''
                }
            }
        }

        stage('Deploy to staging') {
            when { branch 'main' }
            agent { docker { image 'bitnami/kubectl:1.31' } }
            steps {
                withCredentials([file(credentialsId: 'staging-kubeconfig', variable: 'KUBECONFIG')]) {
                    sh '''
                      kubectl apply -k "$(deploy/k8s/render-release.sh staging "$REGISTRY" "$IMAGE_TAG")"
                      for d in fleet-api ingest web simulator; do
                        kubectl -n satfleet-staging rollout status deployment/$d --timeout=300s
                      done
                    '''
                }
            }
        }
    }
}
