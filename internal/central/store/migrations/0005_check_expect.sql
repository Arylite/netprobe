-- What a good answer is, for the kinds of check that take one.
ALTER TABLE checks ADD COLUMN expect text NOT NULL DEFAULT '';
