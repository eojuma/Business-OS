export function getGreeting(): string {
  const now = new Date();
  const h = now.getHours();
  if (h < 12) return "Good morning";
  if (h < 17) return "Good afternoon";
  return "Good evening";
}

export function getGreetingDebug(): { hour: number; greeting: string } {
  const now = new Date();
  const h = now.getHours();
  let greeting = "";
  if (h < 12) greeting = "Good morning";
  else if (h < 17) greeting = "Good afternoon";
  else greeting = "Good evening";
  return { hour: h, greeting };
}

export function getFormattedDate(): string {
  const now = new Date();
  return now.toLocaleDateString("en-GB", {
    weekday: "long",
    day: "numeric",
    month: "long",
  });
}