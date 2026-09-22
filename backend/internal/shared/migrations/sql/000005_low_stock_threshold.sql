-- +migrate Up
-- Any product at or below 200 units is considered low stock. Products that
-- still carry the old zero default are raised to the new floor.
ALTER TABLE stock_levels ALTER COLUMN low_stock_threshold SET DEFAULT 200;
UPDATE stock_levels SET low_stock_threshold = 200 WHERE low_stock_threshold = 0;

-- +migrate Down
UPDATE stock_levels SET low_stock_threshold = 0 WHERE low_stock_threshold = 200;
ALTER TABLE stock_levels ALTER COLUMN low_stock_threshold SET DEFAULT 0;
