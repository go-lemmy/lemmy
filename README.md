<p align="center"><img src="https://raw.githubusercontent.com/go-lemmy/brand/main/social/go-lemmy.png" alt="go-lemmy/lemmy" width="720"></p>

# go-lemmy / lemmy

[![CI](https://github.com/go-lemmy/lemmy/actions/workflows/ci.yml/badge.svg)](https://github.com/go-lemmy/lemmy/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-lemmy/lemmy.svg)](https://pkg.go.dev/github.com/go-lemmy/lemmy)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

A pure-Go (**CGO=0**), dependency-free read client for the [Lemmy](https://join-lemmy.org/) REST API (`/api/v3`).

```go
c := lemmy.New("https://lemmy.world")
list, err := c.Posts(context.Background(), lemmy.PostsOptions{Community: "technology", Sort: "Hot", Limit: 20})
for _, p := range list.Posts {
    fmt.Printf("%s — %s (score %d)\n", p.Title, p.Community, p.Score)
}
```

Optional `Login` stores a JWT for authenticated reads. Stdlib only; builds for all 64-bit targets.

## License

BSD-3-Clause © the go-lemmy/lemmy authors.
