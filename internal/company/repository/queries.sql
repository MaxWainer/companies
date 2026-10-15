-- name: InsertCompany :execrows
INSERT INTO companies (id, name, description, amount_of_employees, registered, type, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT ((lower(name))) DO NOTHING;

-- name: GetCompany :one
SELECT * FROM companies WHERE id = $1;

-- name: GetCompanyForUpdate :one
SELECT * FROM companies WHERE id = $1 FOR UPDATE;

-- name: NameTakenByOther :one
SELECT EXISTS (
    SELECT 1 FROM companies WHERE lower(name) = lower(sqlc.arg(name)) AND id <> sqlc.arg(id)
);

-- name: UpdateCompany :exec
UPDATE companies
SET name = $2, description = $3, amount_of_employees = $4, registered = $5, type = $6, updated_at = $7
WHERE id = $1;

-- name: DeleteCompany :one
DELETE FROM companies WHERE id = $1 RETURNING *;
