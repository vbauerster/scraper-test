# github.com/vbauerster/scraper-test

scraper-test REST API.

## Routes

<details>
<summary>`/admins`</summary>

- [RequestID](https://github.com/go-chi/chi/middleware/request_id.go#L63)
- [RealIP](https://github.com/go-chi/chi/middleware/realip.go#L29)
- [Logger](https://github.com/go-chi/chi/middleware/logger.go#L30)
- [Recoverer](https://github.com/go-chi/chi/middleware/recoverer.go#L18)
- [SetContentType.func1](https://github.com/go-chi/render/content_type.go#L49)
- **/admins**
	- _GET_
		- [(*server).initRoutes.func2](/app/routes.go#L24)

</details>
<details>
<summary>`/bounds`</summary>

- [RequestID](https://github.com/go-chi/chi/middleware/request_id.go#L63)
- [RealIP](https://github.com/go-chi/chi/middleware/realip.go#L29)
- [Logger](https://github.com/go-chi/chi/middleware/logger.go#L30)
- [Recoverer](https://github.com/go-chi/chi/middleware/recoverer.go#L18)
- [SetContentType.func1](https://github.com/go-chi/render/content_type.go#L49)
- **/bounds**
	- _GET_
		- [(*server).queryBounds-fm](/app/handlers.go#L74)

</details>
<details>
<summary>`/services/*/{serviceName}`</summary>

- [RequestID](https://github.com/go-chi/chi/middleware/request_id.go#L63)
- [RealIP](https://github.com/go-chi/chi/middleware/realip.go#L29)
- [Logger](https://github.com/go-chi/chi/middleware/logger.go#L30)
- [Recoverer](https://github.com/go-chi/chi/middleware/recoverer.go#L18)
- [SetContentType.func1](https://github.com/go-chi/render/content_type.go#L49)
- **/services/***
	- **/{serviceName}**
		- _GET_
			- [(*server).queryService-fm](/app/handlers.go#L50)

</details>

Total # of routes: 3
