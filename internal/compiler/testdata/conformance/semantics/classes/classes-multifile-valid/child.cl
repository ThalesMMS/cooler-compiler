class Middle inherits Base {};

class Child inherits Middle {
  identify(x : Int) : Int { x + value + 1 };
};
