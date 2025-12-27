-- tests/sql/sakila-data.sql
-- MySQL Sakila Sample Database Data
-- Sample data for testing purposes

USE sakila;

-- ============================================================
-- Languages
-- ============================================================
INSERT INTO language (language_id, name) VALUES
(1, 'English'),
(2, 'Italian'),
(3, 'Japanese'),
(4, 'Mandarin'),
(5, 'French'),
(6, 'German');

-- ============================================================
-- Categories
-- ============================================================
INSERT INTO category (category_id, name) VALUES
(1, 'Action'),
(2, 'Animation'),
(3, 'Children'),
(4, 'Classics'),
(5, 'Comedy'),
(6, 'Documentary'),
(7, 'Drama'),
(8, 'Family'),
(9, 'Foreign'),
(10, 'Games'),
(11, 'Horror'),
(12, 'Music'),
(13, 'New'),
(14, 'Sci-Fi'),
(15, 'Sports'),
(16, 'Travel');

-- ============================================================
-- Countries
-- ============================================================
INSERT INTO country (country_id, country) VALUES
(1, 'Afghanistan'),
(2, 'Algeria'),
(3, 'American Samoa'),
(4, 'Angola'),
(5, 'Anguilla'),
(6, 'Argentina'),
(7, 'Australia'),
(8, 'Austria'),
(9, 'Azerbaijan'),
(10, 'Bahrain'),
(20, 'Canada'),
(44, 'Germany'),
(49, 'India'),
(50, 'Indonesia'),
(60, 'Japan'),
(75, 'Mexico'),
(82, 'Netherlands'),
(91, 'Russia'),
(103, 'United Kingdom'),
(107, 'United States');

-- ============================================================
-- Cities
-- ============================================================
INSERT INTO city (city_id, city, country_id) VALUES
(1, 'A Corua (La Corua)', 91),
(2, 'Abha', 82),
(3, 'Abu Dhabi', 107),
(4, 'Acua', 75),
(5, 'Adana', 107),
(6, 'Addis Abeba', 107),
(7, 'Ahmadnagar', 49),
(8, 'Akishima', 60),
(9, 'Akron', 107),
(10, 'al-Ayn', 107),
(300, 'Lethbridge', 20),
(312, 'London', 103),
(361, 'Mandi Bahauddin', 49),
(388, 'Nagaon', 49),
(449, 'Rajkot', 49),
(576, 'Woodridge', 7);

-- ============================================================
-- Addresses
-- ============================================================
INSERT INTO address (address_id, address, address2, district, city_id, postal_code, phone) VALUES
(1, '47 MySakila Drive', NULL, 'Alberta', 300, '', ''),
(2, '28 MySQL Boulevard', NULL, 'QLD', 576, '', ''),
(3, '23 Workhaven Lane', NULL, 'Alberta', 300, '', '14033335568'),
(4, '1411 Lillydale Drive', NULL, 'QLD', 576, '', '6172235589'),
(5, '1913 Hanoi Way', NULL, 'Nagasaki', 8, '35200', '28303384290'),
(6, '1121 Loja Avenue', NULL, 'California', 9, '17886', '838635286649'),
(7, '692 Joliet Street', NULL, 'Attika', 312, '83579', '448477190408'),
(8, '1566 Inegl Manor', NULL, 'Mandalay', 312, '53561', '705814003527'),
(9, '53 Idfu Parkway', NULL, 'Nantou', 361, '42399', '10655648674'),
(10, '1795 Santiago de Compostela Way', NULL, 'Texas', 9, '18743', '860452626434');

-- ============================================================
-- Actors
-- ============================================================
INSERT INTO actor (actor_id, first_name, last_name) VALUES
(1, 'PENELOPE', 'GUINESS'),
(2, 'NICK', 'WAHLBERG'),
(3, 'ED', 'CHASE'),
(4, 'JENNIFER', 'DAVIS'),
(5, 'JOHNNY', 'LOLLOBRIGIDA'),
(6, 'BETTE', 'NICHOLSON'),
(7, 'GRACE', 'MOSTEL'),
(8, 'MATTHEW', 'JOHANSSON'),
(9, 'JOE', 'SWANK'),
(10, 'CHRISTIAN', 'GABLE'),
(11, 'ZERO', 'CAGE'),
(12, 'KARL', 'BERRY'),
(13, 'UMA', 'WOOD'),
(14, 'VIVIEN', 'BERGEN'),
(15, 'CUBA', 'OLIVIER'),
(16, 'FRED', 'COSTNER'),
(17, 'HELEN', 'VOIGHT'),
(18, 'DAN', 'TORN'),
(19, 'BOB', 'FAWCETT'),
(20, 'LUCILLE', 'TRACY'),
(21, 'KIRSTEN', 'PALTROW'),
(22, 'ELVIS', 'MARX'),
(23, 'SANDRA', 'KILMER'),
(24, 'CAMERON', 'STREEP'),
(25, 'KEVIN', 'BLOOM'),
(26, 'RIP', 'CRAWFORD'),
(27, 'JULIA', 'MCQUEEN'),
(28, 'WOODY', 'HOFFMAN'),
(29, 'ALEC', 'WAYNE'),
(30, 'SANDRA', 'PECK'),
(31, 'SISSY', 'SOBIESKI'),
(32, 'TIM', 'HACKMAN'),
(33, 'MILLA', 'PECK'),
(34, 'AUDREY', 'OLIVIER'),
(35, 'JUDY', 'DEAN'),
(36, 'BURT', 'DUKAKIS'),
(37, 'VAL', 'BOLGER'),
(38, 'TOM', 'MCKELLEN'),
(39, 'GOLDIE', 'BRODY'),
(40, 'JOHNNY', 'CAGE'),
(41, 'JODIE', 'DEGENERES'),
(42, 'TOM', 'MIRANDA'),
(43, 'KIRK', 'JOVOVICH'),
(44, 'NICK', 'STALLONE'),
(45, 'REESE', 'KILMER'),
(46, 'PARKER', 'GOLDBERG'),
(47, 'JULIA', 'BARRYMORE'),
(48, 'FRANCES', 'DAY-LEWIS'),
(49, 'ANNE', 'CRONYN'),
(50, 'NATALIE', 'HOPKINS');

-- ============================================================
-- Films
-- ============================================================
INSERT INTO film (film_id, title, description, release_year, language_id, original_language_id, rental_duration, rental_rate, length, replacement_cost, rating, special_features) VALUES
(1, 'ACADEMY DINOSAUR', 'A Epic Drama of a Feminist And a Mad Scientist who must Battle a Teacher in The Canadian Rockies', 2006, 1, NULL, 6, 0.99, 86, 20.99, 'PG', 'Deleted Scenes,Behind the Scenes'),
(2, 'ACE GOLDFINGER', 'A Astounding Epistle of a Database Administrator And a Explorer who must Find a Car in Ancient China', 2006, 1, NULL, 3, 4.99, 48, 12.99, 'G', 'Trailers,Deleted Scenes'),
(3, 'ADAPTATION HOLES', 'A Astounding Reflection of a Lumberjack And a Car who must Sink a Lumberjack in A Baloon Factory', 2006, 1, NULL, 7, 2.99, 50, 18.99, 'NC-17', 'Trailers,Deleted Scenes'),
(4, 'AFFAIR PREJUDICE', 'A Fanciful Documentary of a Frisbee And a Lumberjack who must Chase a Monkey in A Shark Tank', 2006, 1, NULL, 5, 2.99, 117, 26.99, 'G', 'Commentaries,Behind the Scenes'),
(5, 'AFRICAN EGG', 'A Fast-Paced Documentary of a Pastry Chef And a Dentist who must Pursue a Forensic Psychologist in The Gulf of Mexico', 2006, 1, NULL, 6, 2.99, 130, 22.99, 'G', 'Deleted Scenes'),
(6, 'AGENT TRUMAN', 'A Intrepid Panorama of a Robot And a Boy who must Escape a Sumo Wrestler in Ancient China', 2006, 1, NULL, 3, 2.99, 169, 17.99, 'PG', 'Deleted Scenes'),
(7, 'AIRPLANE SIERRA', 'A Touching Saga of a Hunter And a Butler who must Discover a Butler in A Jet Boat', 2006, 1, NULL, 6, 4.99, 62, 28.99, 'PG-13', 'Trailers,Deleted Scenes'),
(8, 'AIRPORT POLLOCK', 'A Epic Tale of a Moose And a Girl who must Confront a Monkey in Ancient India', 2006, 1, NULL, 6, 4.99, 54, 15.99, 'R', 'Trailers'),
(9, 'ALABAMA DEVIL', 'A Thoughtful Panorama of a Database Administrator And a Mad Scientist who must Outgun a Mad Scientist in A Jet Boat', 2006, 1, NULL, 3, 2.99, 114, 21.99, 'PG-13', 'Trailers,Deleted Scenes'),
(10, 'ALADDIN CALENDAR', 'A Action-Packed Tale of a Man And a Lumberjack who must Reach a Feminist in Ancient China', 2006, 1, NULL, 6, 4.99, 63, 24.99, 'NC-17', 'Trailers,Deleted Scenes'),
(11, 'ALAMO VIDEOTAPE', 'A Boring Epistle of a Butler And a Cat who must Fight a Pastry Chef in A MySQL Convention', 2006, 1, NULL, 6, 0.99, 126, 16.99, 'G', 'Commentaries,Behind the Scenes'),
(12, 'ALASKA PHANTOM', 'A Fanciful Saga of a Hunter And a Pastry Chef who must Vanquish a Boy in Australia', 2006, 1, NULL, 6, 0.99, 136, 22.99, 'PG', 'Commentaries,Deleted Scenes'),
(13, 'ALI FOREVER', 'A Action-Packed Drama of a Dentist And a Crocodile who must Battle a Feminist in The Canadian Rockies', 2006, 1, NULL, 4, 4.99, 150, 21.99, 'PG', 'Deleted Scenes,Behind the Scenes'),
(14, 'ALICE FANTASIA', 'A Emotional Drama of a A Shark And a Database Administrator who must Vanquish a Pioneer in Soviet Georgia', 2006, 1, NULL, 6, 0.99, 94, 23.99, 'NC-17', 'Trailers,Deleted Scenes,Behind the Scenes'),
(15, 'ALIEN CENTER', 'A Brilliant Drama of a Cat And a Mad Scientist who must Battle a Feminist in A MySQL Convention', 2006, 1, NULL, 5, 2.99, 46, 10.99, 'NC-17', 'Trailers,Commentaries,Behind the Scenes'),
(16, 'ALLEY EVOLUTION', 'A Fast-Paced Drama of a Robot And a Composer who must Battle a Astronaut in New Orleans', 2006, 1, NULL, 6, 2.99, 180, 23.99, 'NC-17', 'Trailers,Commentaries'),
(17, 'ALONE TRIP', 'A Fast-Paced Character Study of a Composer And a Dog who must Outgun a Boat in An Abandoned Fun House', 2006, 1, NULL, 3, 0.99, 82, 14.99, 'R', 'Trailers,Behind the Scenes'),
(18, 'ALTER VICTORY', 'A Thoughtful Drama of a Composer And a Feminist who must Meet a Secret Agent in The Canadian Rockies', 2006, 1, NULL, 6, 0.99, 57, 27.99, 'PG-13', 'Trailers,Behind the Scenes'),
(19, 'AMADEUS HOLY', 'A Emotional Display of a Pioneer And a Technical Writer who must Battle a Man in A Baloon', 2006, 1, NULL, 6, 0.99, 113, 20.99, 'PG', 'Commentaries,Deleted Scenes,Behind the Scenes'),
(20, 'AMELIE HELLFIGHTERS', 'A Boring Drama of a Woman And a Squirrel who must Conquer a Student in A Baloon', 2006, 1, NULL, 4, 4.99, 79, 23.99, 'R', 'Commentaries,Deleted Scenes,Behind the Scenes'),
(21, 'AMERICAN CIRCUS', 'A Insightful Drama of a Girl And a Astronaut who must Face a Database Administrator in A Shark Tank', 2006, 1, NULL, 3, 4.99, 129, 17.99, 'R', 'Commentaries,Behind the Scenes'),
(22, 'AMISTAD MIDSUMMER', 'A Emotional Character Study of a Dentist And a Crocodile who must Meet a Sumo Wrestler in California', 2006, 1, NULL, 6, 2.99, 85, 10.99, 'G', 'Commentaries,Behind the Scenes'),
(23, 'ANACONDA CONFESSIONS', 'A Lacklusture Display of a Dentist And a Dentist who must Fight a Girl in Australia', 2006, 1, NULL, 3, 0.99, 92, 9.99, 'R', 'Trailers,Deleted Scenes'),
(24, 'ANALYZE HOOSIERS', 'A Thoughtful Display of a Explorer And a Pastry Chef who must Overcome a Feminist in The Sahara Desert', 2006, 1, NULL, 6, 2.99, 181, 19.99, 'R', 'Trailers,Behind the Scenes'),
(25, 'ANGELS LIFE', 'A Thoughtful Display of a Woman And a Astronaut who must Battle a Robot in Berlin', 2006, 1, NULL, 3, 2.99, 74, 15.99, 'G', 'Trailers'),
(26, 'ANNIE IDENTITY', 'A Amazing Panorama of a Pastry Chef And a Boat who must Escape a Woman in An Abandoned Amusement Park', 2006, 1, NULL, 3, 0.99, 86, 15.99, 'G', 'Commentaries,Deleted Scenes'),
(27, 'ANONYMOUS HUMAN', 'A Amazing Reflection of a Database Administrator And a Astronaut who must Outrace a Database Administrator in A Shark Tank', 2006, 1, NULL, 7, 0.99, 179, 12.99, 'NC-17', 'Deleted Scenes,Behind the Scenes'),
(28, 'ANTHEM LUKE', 'A Touching Panorama of a Waitress And a Woman who must Outrace a Dog in An Abandoned Amusement Park', 2006, 1, NULL, 5, 4.99, 91, 16.99, 'PG-13', 'Deleted Scenes,Behind the Scenes'),
(29, 'ANTITRUST TOMATOES', 'A Fateful Yarn of a Womanizer And a Feminist who must Succumb a Database Administrator in Ancient India', 2006, 1, NULL, 5, 2.99, 168, 11.99, 'NC-17', 'Trailers,Commentaries,Deleted Scenes'),
(30, 'ANYTHING SAVANNAH', 'A Epic Story of a Pastry Chef And a Woman who must Chase a Feminist in An Abandoned Fun House', 2006, 1, NULL, 4, 2.99, 82, 27.99, 'R', 'Trailers,Deleted Scenes,Behind the Scenes');

-- ============================================================
-- Film Categories
-- ============================================================
INSERT INTO film_category (film_id, category_id) VALUES
(1, 6),
(2, 11),
(3, 6),
(4, 11),
(5, 8),
(6, 9),
(7, 5),
(8, 11),
(9, 11),
(10, 15),
(11, 9),
(12, 12),
(13, 1),
(14, 14),
(15, 9),
(16, 2),
(17, 12),
(18, 7),
(19, 12),
(20, 8),
(21, 1),
(22, 9),
(23, 2),
(24, 6),
(25, 12),
(26, 8),
(27, 2),
(28, 13),
(29, 2),
(30, 1);

-- ============================================================
-- Film Actors
-- ============================================================
INSERT INTO film_actor (actor_id, film_id) VALUES
(1, 1), (1, 23), (1, 25), (1, 27),
(2, 3), (2, 7), (2, 11), (2, 15), (2, 19),
(3, 2), (3, 6), (3, 10), (3, 14), (3, 18), (3, 22), (3, 26), (3, 30),
(4, 1), (4, 5), (4, 9), (4, 13), (4, 17), (4, 21), (4, 25), (4, 29),
(5, 4), (5, 8), (5, 12), (5, 16), (5, 20), (5, 24), (5, 28),
(6, 1), (6, 2), (6, 3), (6, 4), (6, 5),
(7, 6), (7, 7), (7, 8), (7, 9), (7, 10),
(8, 11), (8, 12), (8, 13), (8, 14), (8, 15),
(9, 16), (9, 17), (9, 18), (9, 19), (9, 20),
(10, 21), (10, 22), (10, 23), (10, 24), (10, 25);

-- ============================================================
-- Stores (need to disable FK temporarily)
-- ============================================================
SET FOREIGN_KEY_CHECKS = 0;

INSERT INTO store (store_id, manager_staff_id, address_id) VALUES
(1, 1, 1),
(2, 2, 2);

-- ============================================================
-- Staff
-- ============================================================
INSERT INTO staff (staff_id, first_name, last_name, address_id, email, store_id, active, username, password) VALUES
(1, 'Mike', 'Hillyer', 3, 'Mike.Hillyer@sakilastaff.com', 1, 1, 'Mike', '8cb2237d0679ca88db6464eac60da96345513964'),
(2, 'Jon', 'Stephens', 4, 'Jon.Stephens@sakilastaff.com', 2, 1, 'Jon', '8cb2237d0679ca88db6464eac60da96345513964');

SET FOREIGN_KEY_CHECKS = 1;

-- ============================================================
-- Customers
-- ============================================================
INSERT INTO customer (customer_id, store_id, first_name, last_name, email, address_id, active, create_date) VALUES
(1, 1, 'MARY', 'SMITH', 'MARY.SMITH@sakilacustomer.org', 5, 1, '2006-02-14 22:04:36'),
(2, 1, 'PATRICIA', 'JOHNSON', 'PATRICIA.JOHNSON@sakilacustomer.org', 6, 1, '2006-02-14 22:04:36'),
(3, 1, 'LINDA', 'WILLIAMS', 'LINDA.WILLIAMS@sakilacustomer.org', 7, 1, '2006-02-14 22:04:36'),
(4, 2, 'BARBARA', 'JONES', 'BARBARA.JONES@sakilacustomer.org', 8, 1, '2006-02-14 22:04:36'),
(5, 1, 'ELIZABETH', 'BROWN', 'ELIZABETH.BROWN@sakilacustomer.org', 9, 1, '2006-02-14 22:04:36'),
(6, 2, 'JENNIFER', 'DAVIS', 'JENNIFER.DAVIS@sakilacustomer.org', 10, 1, '2006-02-14 22:04:36'),
(7, 1, 'MARIA', 'MILLER', 'MARIA.MILLER@sakilacustomer.org', 5, 1, '2006-02-14 22:04:36'),
(8, 2, 'SUSAN', 'WILSON', 'SUSAN.WILSON@sakilacustomer.org', 6, 1, '2006-02-14 22:04:36'),
(9, 2, 'MARGARET', 'MOORE', 'MARGARET.MOORE@sakilacustomer.org', 7, 1, '2006-02-14 22:04:36'),
(10, 1, 'DOROTHY', 'TAYLOR', 'DOROTHY.TAYLOR@sakilacustomer.org', 8, 1, '2006-02-14 22:04:36');

-- ============================================================
-- Inventory
-- ============================================================
INSERT INTO inventory (inventory_id, film_id, store_id) VALUES
(1, 1, 1), (2, 1, 1), (3, 1, 1), (4, 1, 1),
(5, 1, 2), (6, 1, 2), (7, 1, 2), (8, 1, 2),
(9, 2, 1), (10, 2, 1), (11, 2, 1),
(12, 2, 2), (13, 2, 2), (14, 2, 2),
(15, 3, 1), (16, 3, 1),
(17, 3, 2), (18, 3, 2), (19, 3, 2),
(20, 4, 1), (21, 4, 1), (22, 4, 1), (23, 4, 1),
(24, 4, 2), (25, 4, 2),
(26, 5, 1), (27, 5, 1), (28, 5, 1),
(29, 5, 2), (30, 5, 2), (31, 5, 2), (32, 5, 2),
(33, 6, 1), (34, 6, 1),
(35, 6, 2), (36, 6, 2), (37, 6, 2),
(38, 7, 1), (39, 7, 1), (40, 7, 1), (41, 7, 1),
(42, 7, 2), (43, 7, 2),
(44, 8, 1), (45, 8, 1),
(46, 8, 2), (47, 8, 2), (48, 8, 2), (49, 8, 2),
(50, 9, 1), (51, 9, 1), (52, 9, 1),
(53, 9, 2), (54, 9, 2),
(55, 10, 1), (56, 10, 1), (57, 10, 1), (58, 10, 1),
(59, 10, 2), (60, 10, 2);

-- ============================================================
-- Rentals
-- ============================================================
INSERT INTO rental (rental_id, rental_date, inventory_id, customer_id, return_date, staff_id) VALUES
(1, '2005-05-24 22:53:30', 1, 1, '2005-05-26 22:04:30', 1),
(2, '2005-05-24 22:54:33', 2, 1, '2005-05-28 19:40:33', 1),
(3, '2005-05-24 23:03:39', 3, 2, '2005-06-01 22:12:39', 1),
(4, '2005-05-24 23:04:41', 9, 2, '2005-06-03 01:43:41', 2),
(5, '2005-05-24 23:05:21', 15, 3, '2005-06-02 04:33:21', 1),
(6, '2005-05-24 23:08:07', 20, 3, '2005-05-27 01:32:07', 1),
(7, '2005-05-24 23:11:53', 26, 4, '2005-05-29 20:34:53', 2),
(8, '2005-05-24 23:31:46', 33, 5, '2005-05-27 23:33:46', 2),
(9, '2005-05-25 00:00:40', 38, 5, '2005-05-31 22:17:40', 1),
(10, '2005-05-25 00:02:21', 44, 6, '2005-05-31 19:47:21', 1),
(11, '2005-05-25 00:09:02', 50, 6, '2005-06-02 18:01:02', 2),
(12, '2005-05-25 00:19:27', 55, 7, '2005-05-28 02:23:27', 1),
(13, '2005-05-25 00:22:55', 5, 7, '2005-06-02 20:51:55', 1),
(14, '2005-05-25 00:31:15', 12, 8, '2005-05-30 05:21:15', 2),
(15, '2005-05-25 00:39:22', 17, 8, '2005-06-03 05:18:22', 2),
(16, '2005-05-25 00:43:11', 24, 9, '2005-06-01 23:10:11', 1),
(17, '2005-05-25 01:06:36', 29, 9, '2005-05-30 08:27:36', 2),
(18, '2005-05-25 01:10:47', 35, 10, '2005-05-29 10:14:47', 1),
(19, '2005-05-25 01:17:24', 42, 10, '2005-05-28 07:37:24', 2),
(20, '2005-05-25 01:48:41', 46, 1, '2005-05-30 09:34:41', 2);

-- ============================================================
-- Payments
-- ============================================================
INSERT INTO payment (payment_id, customer_id, staff_id, rental_id, amount, payment_date) VALUES
(1, 1, 1, 1, 2.99, '2005-05-25 11:30:37'),
(2, 1, 1, 2, 0.99, '2005-05-28 10:35:23'),
(3, 2, 1, 3, 4.99, '2005-06-15 00:54:12'),
(4, 2, 2, 4, 4.99, '2005-06-15 18:02:53'),
(5, 3, 1, 5, 2.99, '2005-06-15 21:08:46'),
(6, 3, 1, 6, 2.99, '2005-06-16 15:18:57'),
(7, 4, 2, 7, 4.99, '2005-06-17 02:11:13'),
(8, 5, 2, 8, 4.99, '2005-06-17 08:35:59'),
(9, 5, 1, 9, 0.99, '2005-06-17 12:28:35'),
(10, 6, 1, 10, 3.99, '2005-06-17 23:51:21'),
(11, 6, 2, 11, 2.99, '2005-06-18 06:14:22'),
(12, 7, 1, 12, 4.99, '2005-06-18 10:41:09'),
(13, 7, 1, 13, 0.99, '2005-06-18 13:33:59'),
(14, 8, 2, 14, 3.99, '2005-06-18 17:05:14'),
(15, 8, 2, 15, 2.99, '2005-06-18 21:37:09'),
(16, 9, 1, 16, 5.99, '2005-06-19 00:02:28'),
(17, 9, 2, 17, 4.99, '2005-06-19 03:24:34'),
(18, 10, 1, 18, 1.99, '2005-06-19 06:44:51'),
(19, 10, 2, 19, 2.99, '2005-06-19 10:17:08'),
(20, 1, 2, 20, 4.99, '2005-06-19 13:29:28');

-- ============================================================
-- Verify data loaded correctly
-- ============================================================
SELECT 'Sakila data loaded successfully' AS status;
SELECT 
    (SELECT COUNT(*) FROM actor) AS actors,
    (SELECT COUNT(*) FROM film) AS films,
    (SELECT COUNT(*) FROM customer) AS customers,
    (SELECT COUNT(*) FROM rental) AS rentals,
    (SELECT COUNT(*) FROM payment) AS payments;

