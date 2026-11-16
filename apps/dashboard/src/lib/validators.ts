import { z } from "zod";
export const emailSchema = z.string().trim().email("Enter a valid email address");
export const requiredText = z.string().trim().min(1, "This field is required");
