# Image Processing Service with Dropbox, RabbitMQ, and PostgreSQL

This project is a Go-based image processing service that integrates RabbitMQ for task queuing, Dropbox for storing and sharing images, and PostgreSQL for database operations. It processes images, uploads them to Dropbox, and updates the PostgreSQL database with shareable links. The logic is split across two main files: `main.go` and `consumer.go`.

---

## Features
- **RabbitMQ Integration:** Handles task queuing for image processing.
- **Dropbox Integration:** Uploads processed images to Dropbox and generates shareable links.
- **PostgreSQL Database:** Tracks product information and updates processed image links.
- **Image Processing:** Fetches, resizes, and compresses images from given URLs.
- **Separation of Concerns:**  
  - `main.go`: Handles RabbitMQ initialization, database setup, and task publishing.
  - `consumer.go`: Consumes tasks from RabbitMQ, processes images, and updates the database.

---

## Prerequisites

1. **Go Programming Language**
   - Install from [Go's official site](https://golang.org/dl/).

2. **RabbitMQ**
   - Install RabbitMQ using [official documentation](https://www.rabbitmq.com/download.html).
   - Ensure RabbitMQ is running locally on `amqp://guest:guest@localhost:5672/`.

3. **PostgreSQL**
   - Set up a PostgreSQL database. For cloud-hosted databases, consider using Render or AWS RDS.

4. **Dropbox**
   - Create a Dropbox app and generate an access token. Follow the [Dropbox API guide](https://www.dropbox.com/developers).

5. **Go Modules**
   - Initialize Go modules using:
     ```bash
     go mod init <module_name>
     go mod tidy
     ```

---

## Setup Instructions

1. **Clone the Repository**
   ```bash
   git clone <repository_url>
   cd <repository_name>
   ```
2. **Environment Variables**
Create a `.env` file with the following keys:
```env
DROPBOX_ACCESS_TOKEN=<Your_Dropbox_Access_Token>
POSTGRES_URL=<Your_PostgreSQL_Connection_URL>
```
3. **Install Dependencies**
```bash
go mod tidy
```
3. **Start RabbitMQ**
    Ensure RabbitMQ is running at localhost:5672.
4. **Start the PostgreSQL Database**
    Ensure your PostgreSQL database is up and running.
4. **Run the Application**
    Build and run the Go services:
    -**main.go**
    ```bash
    go run main.go
    ```
    -**consumer.go**
    ```bash
    go run consumer.go
    ```


