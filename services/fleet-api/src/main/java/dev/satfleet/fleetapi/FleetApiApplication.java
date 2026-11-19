package dev.satfleet.fleetapi;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.context.properties.ConfigurationPropertiesScan;

@SpringBootApplication
@ConfigurationPropertiesScan
public class FleetApiApplication {

    public static void main(String[] args) {
        SpringApplication.run(FleetApiApplication.class, args);
    }
}
