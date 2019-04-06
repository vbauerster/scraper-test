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
		- [(*server).initRoutes.func1](/app/routes.go#L18)

</details>
<details>
<summary>`/bounds/*/errors`</summary>

- [RequestID](https://github.com/go-chi/chi/middleware/request_id.go#L63)
- [RealIP](https://github.com/go-chi/chi/middleware/realip.go#L29)
- [Logger](https://github.com/go-chi/chi/middleware/logger.go#L30)
- [Recoverer](https://github.com/go-chi/chi/middleware/recoverer.go#L18)
- [SetContentType.func1](https://github.com/go-chi/render/content_type.go#L49)
- **/bounds/***
	- **/errors**
		- _GET_
			- [(*server).listErrorServices-fm](/app/handlers.go#L61)

</details>
<details>
<summary>`/bounds/*/max`</summary>

- [RequestID](https://github.com/go-chi/chi/middleware/request_id.go#L63)
- [RealIP](https://github.com/go-chi/chi/middleware/realip.go#L29)
- [Logger](https://github.com/go-chi/chi/middleware/logger.go#L30)
- [Recoverer](https://github.com/go-chi/chi/middleware/recoverer.go#L18)
- [SetContentType.func1](https://github.com/go-chi/render/content_type.go#L49)
- **/bounds/***
	- **/max**
		- _GET_
			- [(*server).boundsMax-fm](/app/handlers.go#L112)

</details>
<details>
<summary>`/bounds/*/min`</summary>

- [RequestID](https://github.com/go-chi/chi/middleware/request_id.go#L63)
- [RealIP](https://github.com/go-chi/chi/middleware/realip.go#L29)
- [Logger](https://github.com/go-chi/chi/middleware/logger.go#L30)
- [Recoverer](https://github.com/go-chi/chi/middleware/recoverer.go#L18)
- [SetContentType.func1](https://github.com/go-chi/render/content_type.go#L49)
- **/bounds/***
	- **/min**
		- _GET_
			- [(*server).boundsMin-fm](/app/handlers.go#L90)

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
			- [(*server).queryService-fm](/app/handlers.go#L68)

</details>

Total # of routes: 5
