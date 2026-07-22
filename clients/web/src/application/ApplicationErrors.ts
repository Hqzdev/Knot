export class LocalIdentityUnavailableError extends Error {
  constructor() {
    super("This browser is not linked to that account");
    this.name = "LocalIdentityUnavailableError";
  }
}
