postgres:
	docker run -e POSTGRES_DB=default -d --rm -it -p 5432:5432 postgres
.PHONY: postgres
