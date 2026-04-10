## ER
```mermaid
erDiagram
    users ||--o{ likes : likes
    posts ||--o{ likes : liked_by
    users {
        UUID id
        VARCHAR(255) email
        VARCHAR(255) hashed_password
        BOOLEAN verified
        TIMESTAMPTZ created_at
    }
    posts {
        UUID id
        UUID users(id)
        TEXT body
        TIMESTAMPTZ created_at
    }
    likes {
        UUID users(id)PK
        UUID posts(id)PK
        TIMESTAMPTZ created_at
    }
```
To process the changes of likes precisely, I made a table `likes` and let `users(id)` and `posts(id)` a composite primary key.

We can reject the duplicated likes by consulting the composite primary key.
