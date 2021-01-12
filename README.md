# inside docker
 ```
docker run -e DOCKER_API_VERSION=1.40 --rm -v /var/run/docker.sock:/var/run/docker.sock bubble --image redis -f 10s 
```
