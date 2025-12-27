-- tests/sql/sakila-schema.sql
-- MySQL Sakila Sample Database Schema
-- Based on MySQL's official Sakila database for testing purposes
-- https://dev.mysql.com/doc/sakila/en/

-- Create the sakila database
CREATE DATABASE IF NOT EXISTS sakila;
USE sakila;

-- ============================================================
-- Table: actor
-- ============================================================
CREATE TABLE actor (
    actor_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
    first_name VARCHAR(45) NOT NULL,
    last_name VARCHAR(45) NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (actor_id),
    KEY idx_actor_last_name (last_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: country
-- ============================================================
CREATE TABLE country (
    country_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
    country VARCHAR(50) NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (country_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: city
-- ============================================================
CREATE TABLE city (
    city_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
    city VARCHAR(50) NOT NULL,
    country_id SMALLINT UNSIGNED NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (city_id),
    KEY idx_fk_country_id (country_id),
    CONSTRAINT fk_city_country FOREIGN KEY (country_id) REFERENCES country (country_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: address
-- ============================================================
CREATE TABLE address (
    address_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
    address VARCHAR(50) NOT NULL,
    address2 VARCHAR(50) DEFAULT NULL,
    district VARCHAR(20) NOT NULL,
    city_id SMALLINT UNSIGNED NOT NULL,
    postal_code VARCHAR(10) DEFAULT NULL,
    phone VARCHAR(20) NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (address_id),
    KEY idx_fk_city_id (city_id),
    CONSTRAINT fk_address_city FOREIGN KEY (city_id) REFERENCES city (city_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: language
-- ============================================================
CREATE TABLE language (
    language_id TINYINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name CHAR(20) NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (language_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: category
-- ============================================================
CREATE TABLE category (
    category_id TINYINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(25) NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (category_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: film
-- ============================================================
CREATE TABLE film (
    film_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
    title VARCHAR(128) NOT NULL,
    description TEXT DEFAULT NULL,
    release_year YEAR DEFAULT NULL,
    language_id TINYINT UNSIGNED NOT NULL,
    original_language_id TINYINT UNSIGNED DEFAULT NULL,
    rental_duration TINYINT UNSIGNED NOT NULL DEFAULT 3,
    rental_rate DECIMAL(4,2) NOT NULL DEFAULT 4.99,
    length SMALLINT UNSIGNED DEFAULT NULL,
    replacement_cost DECIMAL(5,2) NOT NULL DEFAULT 19.99,
    rating ENUM('G','PG','PG-13','R','NC-17') DEFAULT 'G',
    special_features SET('Trailers','Commentaries','Deleted Scenes','Behind the Scenes') DEFAULT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (film_id),
    KEY idx_title (title),
    KEY idx_fk_language_id (language_id),
    KEY idx_fk_original_language_id (original_language_id),
    CONSTRAINT fk_film_language FOREIGN KEY (language_id) REFERENCES language (language_id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT fk_film_language_original FOREIGN KEY (original_language_id) REFERENCES language (language_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: film_actor
-- ============================================================
CREATE TABLE film_actor (
    actor_id SMALLINT UNSIGNED NOT NULL,
    film_id SMALLINT UNSIGNED NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (actor_id, film_id),
    KEY idx_fk_film_id (film_id),
    CONSTRAINT fk_film_actor_actor FOREIGN KEY (actor_id) REFERENCES actor (actor_id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT fk_film_actor_film FOREIGN KEY (film_id) REFERENCES film (film_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: film_category
-- ============================================================
CREATE TABLE film_category (
    film_id SMALLINT UNSIGNED NOT NULL,
    category_id TINYINT UNSIGNED NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (film_id, category_id),
    CONSTRAINT fk_film_category_film FOREIGN KEY (film_id) REFERENCES film (film_id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT fk_film_category_category FOREIGN KEY (category_id) REFERENCES category (category_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: film_text (for FULLTEXT search)
-- ============================================================
CREATE TABLE film_text (
    film_id SMALLINT NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    PRIMARY KEY (film_id),
    FULLTEXT KEY idx_title_description (title, description)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: store
-- ============================================================
CREATE TABLE store (
    store_id TINYINT UNSIGNED NOT NULL AUTO_INCREMENT,
    manager_staff_id TINYINT UNSIGNED NOT NULL,
    address_id SMALLINT UNSIGNED NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (store_id),
    UNIQUE KEY idx_unique_manager (manager_staff_id),
    KEY idx_fk_address_id (address_id),
    CONSTRAINT fk_store_address FOREIGN KEY (address_id) REFERENCES address (address_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: staff
-- ============================================================
CREATE TABLE staff (
    staff_id TINYINT UNSIGNED NOT NULL AUTO_INCREMENT,
    first_name VARCHAR(45) NOT NULL,
    last_name VARCHAR(45) NOT NULL,
    address_id SMALLINT UNSIGNED NOT NULL,
    picture BLOB DEFAULT NULL,
    email VARCHAR(50) DEFAULT NULL,
    store_id TINYINT UNSIGNED NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    username VARCHAR(16) NOT NULL,
    password VARCHAR(40) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (staff_id),
    KEY idx_fk_store_id (store_id),
    KEY idx_fk_address_id (address_id),
    CONSTRAINT fk_staff_address FOREIGN KEY (address_id) REFERENCES address (address_id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT fk_staff_store FOREIGN KEY (store_id) REFERENCES store (store_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Add foreign key to store after staff is created
ALTER TABLE store ADD CONSTRAINT fk_store_staff FOREIGN KEY (manager_staff_id) REFERENCES staff (staff_id) ON DELETE RESTRICT ON UPDATE CASCADE;

-- ============================================================
-- Table: customer
-- ============================================================
CREATE TABLE customer (
    customer_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
    store_id TINYINT UNSIGNED NOT NULL,
    first_name VARCHAR(45) NOT NULL,
    last_name VARCHAR(45) NOT NULL,
    email VARCHAR(50) DEFAULT NULL,
    address_id SMALLINT UNSIGNED NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    create_date DATETIME NOT NULL,
    last_update TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (customer_id),
    KEY idx_fk_store_id (store_id),
    KEY idx_fk_address_id (address_id),
    KEY idx_last_name (last_name),
    CONSTRAINT fk_customer_address FOREIGN KEY (address_id) REFERENCES address (address_id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT fk_customer_store FOREIGN KEY (store_id) REFERENCES store (store_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: inventory
-- ============================================================
CREATE TABLE inventory (
    inventory_id MEDIUMINT UNSIGNED NOT NULL AUTO_INCREMENT,
    film_id SMALLINT UNSIGNED NOT NULL,
    store_id TINYINT UNSIGNED NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (inventory_id),
    KEY idx_fk_film_id (film_id),
    KEY idx_store_id_film_id (store_id, film_id),
    CONSTRAINT fk_inventory_film FOREIGN KEY (film_id) REFERENCES film (film_id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT fk_inventory_store FOREIGN KEY (store_id) REFERENCES store (store_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: rental
-- ============================================================
CREATE TABLE rental (
    rental_id INT NOT NULL AUTO_INCREMENT,
    rental_date DATETIME NOT NULL,
    inventory_id MEDIUMINT UNSIGNED NOT NULL,
    customer_id SMALLINT UNSIGNED NOT NULL,
    return_date DATETIME DEFAULT NULL,
    staff_id TINYINT UNSIGNED NOT NULL,
    last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (rental_id),
    UNIQUE KEY rental_date (rental_date, inventory_id, customer_id),
    KEY idx_fk_inventory_id (inventory_id),
    KEY idx_fk_customer_id (customer_id),
    KEY idx_fk_staff_id (staff_id),
    CONSTRAINT fk_rental_customer FOREIGN KEY (customer_id) REFERENCES customer (customer_id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT fk_rental_inventory FOREIGN KEY (inventory_id) REFERENCES inventory (inventory_id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT fk_rental_staff FOREIGN KEY (staff_id) REFERENCES staff (staff_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Table: payment
-- ============================================================
CREATE TABLE payment (
    payment_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
    customer_id SMALLINT UNSIGNED NOT NULL,
    staff_id TINYINT UNSIGNED NOT NULL,
    rental_id INT DEFAULT NULL,
    amount DECIMAL(5,2) NOT NULL,
    payment_date DATETIME NOT NULL,
    last_update TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (payment_id),
    KEY idx_fk_staff_id (staff_id),
    KEY idx_fk_customer_id (customer_id),
    KEY fk_payment_rental (rental_id),
    CONSTRAINT fk_payment_customer FOREIGN KEY (customer_id) REFERENCES customer (customer_id) ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT fk_payment_rental FOREIGN KEY (rental_id) REFERENCES rental (rental_id) ON DELETE SET NULL ON UPDATE CASCADE,
    CONSTRAINT fk_payment_staff FOREIGN KEY (staff_id) REFERENCES staff (staff_id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ============================================================
-- Views
-- ============================================================

-- View: actor_info - Returns actor information with their films
CREATE OR REPLACE VIEW actor_info AS
SELECT
    a.actor_id,
    a.first_name,
    a.last_name,
    GROUP_CONCAT(DISTINCT CONCAT(c.name, ': ',
        (SELECT GROUP_CONCAT(f.title ORDER BY f.title SEPARATOR ', ')
         FROM film f
         INNER JOIN film_category fc ON f.film_id = fc.film_id
         INNER JOIN film_actor fa ON f.film_id = fa.film_id
         WHERE fc.category_id = c.category_id AND fa.actor_id = a.actor_id)
    ) ORDER BY c.name SEPARATOR '; ') AS film_info
FROM actor a
LEFT JOIN film_actor fa ON a.actor_id = fa.actor_id
LEFT JOIN film_category fc ON fa.film_id = fc.film_id
LEFT JOIN category c ON fc.category_id = c.category_id
GROUP BY a.actor_id, a.first_name, a.last_name;

-- View: customer_list - Returns formatted customer list
CREATE OR REPLACE VIEW customer_list AS
SELECT
    cu.customer_id AS ID,
    CONCAT(cu.first_name, ' ', cu.last_name) AS name,
    a.address,
    a.postal_code AS `zip code`,
    a.phone,
    city.city,
    country.country,
    IF(cu.active, 'active', '') AS notes,
    cu.store_id AS SID
FROM customer cu
JOIN address a ON cu.address_id = a.address_id
JOIN city ON a.city_id = city.city_id
JOIN country ON city.country_id = country.country_id;

-- View: film_list - Returns film information with category and actors
CREATE OR REPLACE VIEW film_list AS
SELECT
    f.film_id AS FID,
    f.title,
    f.description,
    category.name AS category,
    f.rental_rate AS price,
    f.length,
    f.rating,
    GROUP_CONCAT(CONCAT(a.first_name, ' ', a.last_name) SEPARATOR ', ') AS actors
FROM film f
LEFT JOIN film_category fc ON f.film_id = fc.film_id
LEFT JOIN category ON fc.category_id = category.category_id
LEFT JOIN film_actor fa ON f.film_id = fa.film_id
LEFT JOIN actor a ON fa.actor_id = a.actor_id
GROUP BY f.film_id, f.title, f.description, category.name, f.rental_rate, f.length, f.rating;

-- View: staff_list - Returns staff information
CREATE OR REPLACE VIEW staff_list AS
SELECT
    s.staff_id AS ID,
    CONCAT(s.first_name, ' ', s.last_name) AS name,
    a.address,
    a.postal_code AS `zip code`,
    a.phone,
    city.city,
    country.country,
    s.store_id AS SID
FROM staff s
JOIN address a ON s.address_id = a.address_id
JOIN city ON a.city_id = city.city_id
JOIN country ON city.country_id = country.country_id;

-- View: sales_by_store - Returns sales statistics by store
CREATE OR REPLACE VIEW sales_by_store AS
SELECT
    CONCAT(c.city, ', ', cy.country) AS store,
    CONCAT(m.first_name, ' ', m.last_name) AS manager,
    SUM(p.amount) AS total_sales
FROM payment p
INNER JOIN rental r ON p.rental_id = r.rental_id
INNER JOIN inventory i ON r.inventory_id = i.inventory_id
INNER JOIN store s ON i.store_id = s.store_id
INNER JOIN address a ON s.address_id = a.address_id
INNER JOIN city c ON a.city_id = c.city_id
INNER JOIN country cy ON c.country_id = cy.country_id
INNER JOIN staff m ON s.manager_staff_id = m.staff_id
GROUP BY s.store_id, c.city, cy.country, m.first_name, m.last_name;

-- View: sales_by_film_category - Returns sales statistics by film category
CREATE OR REPLACE VIEW sales_by_film_category AS
SELECT
    c.name AS category,
    SUM(p.amount) AS total_sales
FROM payment p
INNER JOIN rental r ON p.rental_id = r.rental_id
INNER JOIN inventory i ON r.inventory_id = i.inventory_id
INNER JOIN film f ON i.film_id = f.film_id
INNER JOIN film_category fc ON f.film_id = fc.film_id
INNER JOIN category c ON fc.category_id = c.category_id
GROUP BY c.name
ORDER BY total_sales DESC;

-- ============================================================
-- Stored Functions (for testing that we block them)
-- ============================================================

DELIMITER //

CREATE FUNCTION get_customer_balance(p_customer_id INT, p_effective_date DATETIME)
RETURNS DECIMAL(5,2)
DETERMINISTIC
READS SQL DATA
BEGIN
    DECLARE v_rentfees DECIMAL(5,2);
    DECLARE v_overfees INT;
    DECLARE v_payments DECIMAL(5,2);

    SELECT IFNULL(SUM(f.rental_rate), 0) INTO v_rentfees
    FROM film f
    INNER JOIN inventory i ON f.film_id = i.film_id
    INNER JOIN rental r ON i.inventory_id = r.inventory_id
    WHERE r.rental_date <= p_effective_date
      AND r.customer_id = p_customer_id;

    SELECT IFNULL(SUM(IF((TO_DAYS(r.return_date) - TO_DAYS(r.rental_date)) > f.rental_duration,
        (TO_DAYS(r.return_date) - TO_DAYS(r.rental_date)) - f.rental_duration, 0)), 0) INTO v_overfees
    FROM rental r
    INNER JOIN inventory i ON r.inventory_id = i.inventory_id
    INNER JOIN film f ON i.film_id = f.film_id
    WHERE r.rental_date <= p_effective_date
      AND r.customer_id = p_customer_id;

    SELECT IFNULL(SUM(p.amount), 0) INTO v_payments
    FROM payment p
    WHERE p.payment_date <= p_effective_date
      AND p.customer_id = p_customer_id;

    RETURN v_rentfees + v_overfees - v_payments;
END //

-- Stored Procedure for testing (should be blocked by MCP server)
CREATE PROCEDURE film_in_stock(IN p_film_id INT, IN p_store_id INT, OUT p_film_count INT)
READS SQL DATA
BEGIN
    SELECT COUNT(*)
    FROM inventory
    WHERE film_id = p_film_id
      AND store_id = p_store_id
      AND inventory_in_stock(inventory_id);

    SELECT COUNT(*)
    INTO p_film_count
    FROM inventory
    WHERE film_id = p_film_id
      AND store_id = p_store_id
      AND inventory_in_stock(inventory_id);
END //

-- Helper function for film_in_stock
CREATE FUNCTION inventory_in_stock(p_inventory_id INT)
RETURNS BOOLEAN
READS SQL DATA
BEGIN
    DECLARE v_rentals INT;
    DECLARE v_out INT;

    SELECT COUNT(*) INTO v_rentals
    FROM rental
    WHERE inventory_id = p_inventory_id;

    IF v_rentals = 0 THEN
        RETURN TRUE;
    END IF;

    SELECT COUNT(r.rental_id) INTO v_out
    FROM inventory i
    INNER JOIN rental r ON i.inventory_id = r.inventory_id
    WHERE i.inventory_id = p_inventory_id
      AND r.return_date IS NULL;

    IF v_out > 0 THEN
        RETURN FALSE;
    ELSE
        RETURN TRUE;
    END IF;
END //

DELIMITER ;

-- ============================================================
-- Triggers (demonstrating full schema)
-- ============================================================

-- Trigger to update film_text table when film is inserted
DELIMITER //

CREATE TRIGGER ins_film
AFTER INSERT ON film
FOR EACH ROW
BEGIN
    INSERT INTO film_text (film_id, title, description)
    VALUES (NEW.film_id, NEW.title, NEW.description);
END //

CREATE TRIGGER upd_film
AFTER UPDATE ON film
FOR EACH ROW
BEGIN
    IF (OLD.title != NEW.title) OR (OLD.description != NEW.description) OR (OLD.film_id != NEW.film_id) THEN
        UPDATE film_text
        SET title = NEW.title, description = NEW.description, film_id = NEW.film_id
        WHERE film_id = OLD.film_id;
    END IF;
END //

CREATE TRIGGER del_film
AFTER DELETE ON film
FOR EACH ROW
BEGIN
    DELETE FROM film_text WHERE film_id = OLD.film_id;
END //

DELIMITER ;

