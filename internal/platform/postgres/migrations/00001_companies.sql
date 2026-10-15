-- +goose Up
CREATE TABLE companies (
    id                  uuid          PRIMARY KEY,
    name                varchar(15)   NOT NULL,
    description         varchar(3000),
    amount_of_employees integer       NOT NULL CHECK (amount_of_employees >= 0),
    registered          boolean       NOT NULL,
    type                text          NOT NULL
        CHECK (type IN ('CORPORATIONS', 'NON_PROFIT', 'COOPERATIVE', 'SOLE_PROPRIETORSHIP')),
    created_at          timestamptz   NOT NULL,
    updated_at          timestamptz   NOT NULL
);

CREATE UNIQUE INDEX companies_name_key ON companies (lower(name));

-- +goose Down
DROP TABLE companies;
