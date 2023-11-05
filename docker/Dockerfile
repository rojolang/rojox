# Start from the latest golang base image
FROM golang:latest

# Add Maintainer Info
LABEL maintainer="rojo <https://github.com/rojolang>"

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source from the current directory to the Working Directory inside the container
COPY . .

# Set a new working directory to build the Go app
WORKDIR /build

# Copy the source again from the /app directory to the /build directory inside the container
COPY . .

# Build the Go app
RUN go build -o main ./main.go

# Move back to the /app directory
WORKDIR /app

# Move the built application from the /build directory to the /app directory
RUN mv /build/main /app/main

# Expose port 8080 to the outside world
EXPOSE 8080

# Command to run the executable
CMD ["./main", "--ux"]
