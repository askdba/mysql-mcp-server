-- tests/sql/init.sql
-- Database initialization script for integration tests
-- This file is mounted into MySQL containers at startup

-- Create test database (if not exists via env var)
CREATE DATABASE IF NOT EXISTS testdb;
USE testdb;

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE,
    status ENUM('active', 'inactive', 'pending') DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_status (status),
    INDEX idx_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Orders table (for JOIN tests)
CREATE TABLE IF NOT EXISTS orders (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id INT NOT NULL,
    total DECIMAL(10, 2) NOT NULL,
    status ENUM('pending', 'completed', 'cancelled') DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    INDEX idx_user_id (user_id),
    INDEX idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Products table (for aggregation tests)
CREATE TABLE IF NOT EXISTS products (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    category VARCHAR(100),
    price DECIMAL(10, 2) NOT NULL,
    stock INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_category (category)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Table with special characters (for edge case tests)
CREATE TABLE IF NOT EXISTS special_data (
    id INT AUTO_INCREMENT PRIMARY KEY,
    unicode_text VARCHAR(255) CHARACTER SET utf8mb4,
    json_data JSON,
    binary_data BLOB,
    large_text LONGTEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Insert test data
INSERT INTO users (name, email, status) VALUES
    ('Alice', 'alice@example.com', 'active'),
    ('Bob', 'bob@example.com', 'active'),
    ('Charlie', 'charlie@example.com', 'inactive'),
    ('Diana', 'diana@example.com', 'pending'),
    ('Eve', 'eve@example.com', 'active');

INSERT INTO orders (user_id, total, status) VALUES
    (1, 99.99, 'completed'),
    (1, 149.50, 'completed'),
    (2, 75.00, 'pending'),
    (3, 200.00, 'cancelled'),
    (5, 50.25, 'completed');

INSERT INTO products (name, category, price, stock) VALUES
    ('Laptop', 'Electronics', 999.99, 50),
    ('Mouse', 'Electronics', 29.99, 200),
    ('Keyboard', 'Electronics', 79.99, 150),
    ('Desk', 'Furniture', 299.99, 30),
    ('Chair', 'Furniture', 199.99, 45),
    ('Monitor', 'Electronics', 399.99, 75);

-- Insert special character data for edge case testing
INSERT INTO special_data (unicode_text, json_data, large_text) VALUES
    ('日本語テスト', '{"key": "value", "nested": {"a": 1}}', REPEAT('a', 1000)),
    ('Ñoño español', '{"emoji": "🎉", "flag": "🇺🇸"}', REPEAT('b', 1000)),
    ('中文测试', '{"array": [1, 2, 3]}', REPEAT('c', 1000)),
    ('; DROP TABLE users; --', '{"injection": "test"}', 'SQL injection test string');

-- Create a view for testing SHOW commands
CREATE OR REPLACE VIEW user_orders AS
SELECT u.id, u.name, COUNT(o.id) as order_count, COALESCE(SUM(o.total), 0) as total_spent
FROM users u
LEFT JOIN orders o ON u.id = o.user_id
GROUP BY u.id, u.name;

-- Create a stored procedure (for testing that CALL is blocked)
DELIMITER //
CREATE PROCEDURE IF NOT EXISTS get_user_by_id(IN user_id INT)
BEGIN
    SELECT * FROM users WHERE id = user_id;
END //
DELIMITER ;

-- Grant permissions to test user
GRANT SELECT ON testdb.* TO 'testuser'@'%';
FLUSH PRIVILEGES;

